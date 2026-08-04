// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package node

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/internal/testlog"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

func init() {
	rpc.RegisterRestrictedDebugFactory(func(service interface{}, opts rpc.RestrictedDebugOptions) (interface{}, error) {
		if _, ok := service.(*fakeDebugService); !ok {
			return nil, nil
		}
		return new(fakeRestrictedDebugService), nil
	})
}

// fakeDebugService stands in for the union of the real debug services: the two
// methods the indexer needs alongside the destructive ones it must not reach.
type fakeDebugService struct{}

func (s *fakeDebugService) TraceTransaction() string   { return "ok" }
func (s *fakeDebugService) TraceBlockByNumber() string { return "ok" }
func (s *fakeDebugService) TraceCall() string          { return "ok" }
func (s *fakeDebugService) TraceBlockByHash() string   { return "ok" }
func (s *fakeDebugService) TraceBlockFromFile() string { return "ok" }
func (s *fakeDebugService) SetHead() string            { return "ok" }
func (s *fakeDebugService) StartCPUProfile() string    { return "ok" }
func (s *fakeDebugService) ChaindbCompact() string     { return "ok" }

// fakeRestrictedDebugService is the restricted view of fakeDebugService.
type fakeRestrictedDebugService struct{}

func (s *fakeRestrictedDebugService) TraceTransaction() string   { return "ok" }
func (s *fakeRestrictedDebugService) TraceBlockByNumber() string { return "ok" }

// unrestrictableDebugService is a debug service no factory recognises.
type unrestrictableDebugService struct{}

func (s *unrestrictableDebugService) SetGCPercent() string { return "ok" }

func TestDebugTransportValidate(t *testing.T) {
	tests := []struct {
		name      string
		transport debugTransport
		wantErr   bool
	}{
		{
			name:      "debug without profile or opt-in",
			transport: debugTransport{name: "http", modules: []string{"eth", "debug"}},
			wantErr:   true,
		},
		{
			name:      "debug with profile",
			transport: debugTransport{name: "http", modules: []string{"eth", "debug"}, profile: rpc.DebugProfileTraceIndexerV1},
		},
		{
			name:      "debug with unsafe opt-in",
			transport: debugTransport{name: "http", modules: []string{"eth", "debug"}, allowUnsafe: true},
		},
		{
			name:      "profile and unsafe opt-in together",
			transport: debugTransport{name: "http", modules: []string{"eth", "debug"}, profile: rpc.DebugProfileTraceIndexerV1, allowUnsafe: true},
			wantErr:   true,
		},
		{
			name:      "profile without debug in api",
			transport: debugTransport{name: "http", modules: []string{"eth"}, profile: rpc.DebugProfileTraceIndexerV1},
			wantErr:   true,
		},
		{
			name:      "unknown profile",
			transport: debugTransport{name: "http", modules: []string{"eth", "debug"}, profile: "trace-indexer-v2"},
			wantErr:   true,
		},
		{
			name:      "no debug namespace",
			transport: debugTransport{name: "http", modules: []string{"eth", "net", "web3"}},
		},
		{
			// An empty module list registers every namespace, debug among them,
			// but the operator never named debug: that is warned about at
			// startup, not made fatal.
			name:      "empty module list without profile or opt-in",
			transport: debugTransport{name: "http", modules: nil},
		},
		{
			name:      "empty module list with profile",
			transport: debugTransport{name: "http", modules: nil, profile: rpc.DebugProfileTraceIndexerV1},
		},
		{
			name:      "empty module list with unsafe opt-in",
			transport: debugTransport{name: "http", modules: nil, allowUnsafe: true},
		},
		{
			name:      "unsafe opt-in without debug is harmless",
			transport: debugTransport{name: "ws", modules: []string{"eth"}, allowUnsafe: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.transport.validate()
			if tt.wantErr && err == nil {
				t.Fatal("configuration accepted, want a startup error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("configuration rejected: %v", err)
			}
		})
	}
}

func TestCheckDebugProfilesSkipsDisabledTransports(t *testing.T) {
	// IPC keeps the full debug namespace and no transport is enabled here, so
	// an otherwise fatal module list must not block startup.
	config := &Config{
		HTTPModules: []string{"debug"},
		WSModules:   []string{"debug"},
	}
	if err := config.checkDebugProfiles(); err != nil {
		t.Fatalf("disabled transports rejected: %v", err)
	}

	config.HTTPHost = "127.0.0.1"
	if err := config.checkDebugProfiles(); err == nil {
		t.Fatal("enabled HTTP transport with unguarded debug accepted")
	}

	config.HTTPDebugProfile = rpc.DebugProfileTraceIndexerV1
	if err := config.checkDebugProfiles(); err != nil {
		t.Fatalf("profiled HTTP transport rejected: %v", err)
	}

	// WS must be covered as thoroughly as HTTP.
	config.WSHost = "127.0.0.1"
	if err := config.checkDebugProfiles(); err == nil {
		t.Fatal("enabled WS transport with unguarded debug accepted")
	}
}

func TestDebugTraceLimiterIsShared(t *testing.T) {
	config := &Config{}
	if config.debugTraceLimiter() != nil {
		t.Fatal("limiter created without any profile")
	}

	config.HTTPDebugProfile = rpc.DebugProfileTraceIndexerV1
	config.DebugTraceMaxConcurrency = 1
	limiter := config.debugTraceLimiter()
	if limiter == nil {
		t.Fatal("no limiter created for a profiled transport")
	}
	// Both transports hand out slots from the same limiter.
	httpOpts := config.httpDebugTransport().options(limiter)
	config.WSDebugProfile = rpc.DebugProfileTraceIndexerV1
	wsOpts := config.wsDebugTransport().options(limiter)
	if httpOpts.Limiter != wsOpts.Limiter {
		t.Fatal("HTTP and WS use separate limiters")
	}

	release, err := httpOpts.Limiter.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wsOpts.Limiter.Acquire(); err != rpc.ErrTooManyTraces {
		t.Fatalf("WS acquire = %v, want ErrTooManyTraces", err)
	}
	release()
}

func TestRestrictedDebugHTTPTransport(t *testing.T) {
	srv := startDebugServer(t, &rpc.RestrictedDebugOptions{Profile: rpc.DebugProfileTraceIndexerV1})
	url := "http://" + srv.listenAddr()

	// The namespace stays visible: rpc_modules reports namespaces, not
	// per-method capabilities.
	var modules map[string]string
	if err := json.Unmarshal(callDebug(t, url, "rpc_modules").Result, &modules); err != nil {
		t.Fatal(err)
	}
	if _, ok := modules["debug"]; !ok {
		t.Fatalf("debug namespace missing from rpc_modules: %v", modules)
	}

	for _, method := range []string{"debug_traceTransaction", "debug_traceBlockByNumber"} {
		if resp := callDebug(t, url, method); resp.Error != nil {
			t.Fatalf("%s: %v", method, resp.Error)
		}
	}
	for _, method := range []string{
		"debug_setHead", "debug_startCPUProfile", "debug_chaindbCompact",
		"debug_traceCall", "debug_traceBlockByHash", "debug_traceBlockFromFile",
	} {
		resp := callDebug(t, url, method)
		if resp.Error == nil {
			t.Fatalf("%s is reachable on a restricted transport", method)
		}
		if resp.Error.Code != -32601 {
			t.Fatalf("%s: code = %d, want -32601", method, resp.Error.Code)
		}
	}
}

func TestUnrestrictedDebugHTTPTransport(t *testing.T) {
	// Without a profile the full namespace is registered, exactly as before.
	srv := startDebugServer(t, nil)
	url := "http://" + srv.listenAddr()

	for _, method := range []string{
		"debug_traceTransaction", "debug_traceBlockByNumber", "debug_setHead",
		"debug_startCPUProfile", "debug_chaindbCompact", "debug_traceCall",
		"debug_traceBlockByHash", "debug_traceBlockFromFile",
	} {
		if resp := callDebug(t, url, method); resp.Error != nil {
			t.Fatalf("%s missing on an unrestricted transport: %v", method, resp.Error)
		}
	}
}

func TestRestrictedDebugDropsUnrestrictableService(t *testing.T) {
	// A debug service no factory can restrict must be dropped, not registered.
	// Another service in the same namespace still satisfies the profile.
	apis := []rpc.API{
		{Namespace: "debug", Service: new(unrestrictableDebugService)},
		{Namespace: "debug", Service: new(fakeDebugService)},
	}
	srv := newHTTPServer(testlog.Logger(t, log.LvlDebug), rpc.DefaultHTTPTimeouts)
	config := httpConfig{
		Modules:      []string{"debug"},
		debugProfile: &rpc.RestrictedDebugOptions{Profile: rpc.DebugProfileTraceIndexerV1},
	}
	if err := srv.enableRPC(apis, config); err != nil {
		t.Fatal(err)
	}
	if err := srv.setListenAddr("localhost", 0); err != nil {
		t.Fatal(err)
	}
	if err := srv.start(); err != nil {
		t.Fatal(err)
	}
	defer srv.stop()

	url := "http://" + srv.listenAddr()
	if resp := callDebug(t, url, "debug_traceTransaction"); resp.Error != nil {
		t.Fatalf("profile method unavailable: %v", resp.Error)
	}
	resp := callDebug(t, url, "debug_setGCPercent")
	if resp.Error == nil {
		t.Fatal("unrestrictable service was registered on a restricted transport")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("code = %d, want -32601", resp.Error.Code)
	}
}

func TestRestrictedDebugFailsWhenNoServiceCanServeIt(t *testing.T) {
	// Asking for a profile nothing can serve is a startup error, never a
	// silently unrestricted or silently empty namespace.
	apis := []rpc.API{{Namespace: "debug", Service: new(unrestrictableDebugService)}}
	srv := newHTTPServer(testlog.Logger(t, log.LvlDebug), rpc.DefaultHTTPTimeouts)
	err := srv.enableRPC(apis, httpConfig{
		Modules:      []string{"debug"},
		debugProfile: &rpc.RestrictedDebugOptions{Profile: rpc.DebugProfileTraceIndexerV1},
	})
	if err == nil {
		t.Fatal("unservable profile accepted")
	}
}

func TestRestrictedDebugBatchTraceLimit(t *testing.T) {
	srv := startDebugServer(t, &rpc.RestrictedDebugOptions{Profile: rpc.DebugProfileTraceIndexerV1})
	url := "http://" + srv.listenAddr()

	methods := make([]string, rpc.MaxTraceCallsPerBatch)
	for i := range methods {
		methods[i] = "debug_traceTransaction"
	}
	for _, resp := range callDebugBatch(t, url, methods) {
		if resp.Error != nil {
			t.Fatalf("batch of %d traces rejected: %v", len(methods), resp.Error)
		}
	}

	methods = append(methods, "debug_traceBlockByNumber")
	for _, resp := range callDebugBatch(t, url, methods) {
		if resp.Error == nil {
			t.Fatalf("batch of %d traces accepted", len(methods))
		}
	}
}

// startDebugServer starts an HTTP server exposing fakeDebugService under the
// debug namespace, restricted by the given options when non-nil.
func startDebugServer(t *testing.T, debugProfile *rpc.RestrictedDebugOptions) *httpServer {
	t.Helper()

	srv := newHTTPServer(testlog.Logger(t, log.LvlDebug), rpc.DefaultHTTPTimeouts)
	config := httpConfig{
		Modules:      []string{"debug"},
		batchLimit:   DefaultBatchLimit,
		debugProfile: debugProfile,
	}
	apis := []rpc.API{{Namespace: "debug", Service: new(fakeDebugService)}}
	if err := srv.enableRPC(apis, config); err != nil {
		t.Fatal(err)
	}
	if err := srv.setListenAddr("localhost", 0); err != nil {
		t.Fatal(err)
	}
	if err := srv.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.stop)
	return srv
}

// jsonrpcResponse is the part of a JSON-RPC response these tests inspect.
type jsonrpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func callDebug(t *testing.T, url, method string) jsonrpcResponse {
	t.Helper()

	var resp jsonrpcResponse
	decodeRPC(t, rpcRequest(t, url, method), &resp)
	return resp
}

func callDebugBatch(t *testing.T, url string, methods []string) []jsonrpcResponse {
	t.Helper()

	var resp []jsonrpcResponse
	decodeRPC(t, batchRpcRequest(t, url, methods), &resp)
	return resp
}

func decodeRPC(t *testing.T, resp *http.Response, into interface{}) {
	t.Helper()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("cannot decode %s: %v", body, err)
	}
}
