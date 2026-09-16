// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package rpc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// fullDebugService stands in for the union of the real debug services: a couple
// of profile methods next to the destructive ones the profile must hide.
type fullDebugService struct{}

func (s *fullDebugService) TraceTransaction() string   { return "traced" }
func (s *fullDebugService) TraceBlockByNumber() string { return "traced" }
func (s *fullDebugService) TraceCall() string          { return "traced" }
func (s *fullDebugService) SetHead() string            { return "rewound" }
func (s *fullDebugService) StartCPUProfile() string    { return "profiling" }
func (s *fullDebugService) ChaindbCompact() string     { return "compacted" }

func TestDebugProfileMethods(t *testing.T) {
	methods, err := DebugProfileMethods(DebugProfileTraceIndexerV1)
	if err != nil {
		t.Fatalf("profile %s must exist: %v", DebugProfileTraceIndexerV1, err)
	}
	want := []string{"traceBlockByNumber", "traceTransaction"}
	if !reflect.DeepEqual(methods, want) {
		t.Fatalf("profile methods = %v, want %v", methods, want)
	}
}

func TestDebugProfileUnknownFails(t *testing.T) {
	// An unrecognised profile must be an error, never a wider surface.
	if _, err := DebugProfileMethods("trace-indexer-v2"); err == nil {
		t.Fatal("unknown profile accepted")
	}
	if _, err := DebugProfileMethods(""); err == nil {
		t.Fatal("empty profile accepted")
	}
}

func TestDebugProfileMethodsAreACopy(t *testing.T) {
	methods, err := DebugProfileMethods(DebugProfileTraceIndexerV1)
	if err != nil {
		t.Fatal(err)
	}
	methods[0] = "setHead"

	again, err := DebugProfileMethods(DebugProfileTraceIndexerV1)
	if err != nil {
		t.Fatal(err)
	}
	if again[0] == "setHead" {
		t.Fatal("caller mutated the profile table")
	}
}

func TestRegisterNameWithMethods(t *testing.T) {
	allowed, err := DebugProfileMethods(DebugProfileTraceIndexerV1)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer()
	if err := server.RegisterNameWithMethods(DebugNamespace, new(fullDebugService), allowed); err != nil {
		t.Fatalf("registration failed: %v", err)
	}
	client := DialInProc(server)
	defer client.Close()

	// The namespace stays visible: rpc_modules reports namespaces, not
	// per-method capabilities.
	var modules map[string]string
	if err := client.Call(&modules, "rpc_modules"); err != nil {
		t.Fatal(err)
	}
	if _, ok := modules[DebugNamespace]; !ok {
		t.Fatalf("debug namespace missing from rpc_modules: %v", modules)
	}

	for _, method := range []string{"debug_traceTransaction", "debug_traceBlockByNumber"} {
		var result string
		if err := client.Call(&result, method); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	for _, method := range []string{"debug_traceCall", "debug_setHead", "debug_startCPUProfile", "debug_chaindbCompact"} {
		var result string
		err := client.Call(&result, method)
		if err == nil {
			t.Fatalf("%s was registered on a restricted transport", method)
		}
		rpcErr, ok := err.(Error)
		if !ok {
			t.Fatalf("%s: error %v is not a JSON-RPC error", method, err)
		}
		if rpcErr.ErrorCode() != -32601 {
			t.Fatalf("%s: code = %d, want -32601", method, rpcErr.ErrorCode())
		}
	}
}

func TestRegisterNameWithMethodsRejectsMissingMethod(t *testing.T) {
	// A profile naming a method the receiver does not have must fail loudly at
	// startup rather than register a smaller surface than intended.
	server := NewServer()
	err := server.RegisterNameWithMethods(DebugNamespace, new(fullDebugService), []string{"traceTransaction", "traceGone"})
	if err == nil {
		t.Fatal("registration of an absent method succeeded")
	}
	if !strings.Contains(err.Error(), "traceGone") {
		t.Fatalf("error does not name the missing method: %v", err)
	}
}

func TestRegisterNameRegistersEverything(t *testing.T) {
	// Unrestricted registration — what IPC uses — is unchanged.
	server := NewServer()
	if err := server.RegisterName(DebugNamespace, new(fullDebugService)); err != nil {
		t.Fatal(err)
	}
	client := DialInProc(server)
	defer client.Close()

	for _, method := range []string{
		"debug_traceTransaction", "debug_traceBlockByNumber", "debug_traceCall",
		"debug_setHead", "debug_startCPUProfile", "debug_chaindbCompact",
	} {
		var result string
		if err := client.Call(&result, method); err != nil {
			t.Fatalf("%s missing on an unrestricted transport: %v", method, err)
		}
	}
}

func TestIsProfiledTraceMethod(t *testing.T) {
	tests := map[string]bool{
		"debug_traceTransaction":   true,
		"debug_traceBlockByNumber": true,
		"debug_traceCall":          false,
		"debug_setHead":            false,
		"eth_traceTransaction":     false,
		"traceTransaction":         false,
		"":                         false,
	}
	for method, want := range tests {
		if got := isProfiledTraceMethod(method); got != want {
			t.Errorf("isProfiledTraceMethod(%q) = %v, want %v", method, got, want)
		}
	}
}

func TestTraceLimiter(t *testing.T) {
	limiter := NewTraceLimiter(2)

	first, err := limiter.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	second, err := limiter.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	// Saturation is reported immediately rather than queued.
	if _, err := limiter.Acquire(); err != ErrTooManyTraces {
		t.Fatalf("third acquire = %v, want ErrTooManyTraces", err)
	}
	if code := ErrTooManyTraces.(Error).ErrorCode(); code != -32005 {
		t.Fatalf("ErrTooManyTraces code = %d, want -32005", code)
	}

	first()
	if release, err := limiter.Acquire(); err != nil {
		t.Fatalf("acquire after release: %v", err)
	} else {
		release()
	}
	second()
}

func TestTraceLimiterUnlimited(t *testing.T) {
	// A nil limiter means no limit and must stay usable.
	var limiter *TraceLimiter
	if NewTraceLimiter(0) != nil {
		t.Fatal("NewTraceLimiter(0) should be unlimited")
	}
	for i := 0; i < 100; i++ {
		if _, err := limiter.Acquire(); err != nil {
			t.Fatalf("nil limiter refused acquire: %v", err)
		}
	}
}

// batchService counts how many calls actually made it through.
type batchService struct{ calls int }

func (s *batchService) TraceTransaction() string   { s.calls++; return "traced" }
func (s *batchService) TraceBlockByNumber() string { s.calls++; return "traced" }
func (s *batchService) Ping() string               { s.calls++; return "pong" }

func TestBatchLimits(t *testing.T) {
	tests := []struct {
		name       string
		itemLimit  int
		traceLimit int
		methods    []string
		wantErr    bool
	}{
		{
			name:      "within item limit",
			itemLimit: 3,
			methods:   []string{"debug_ping", "debug_ping", "debug_ping"},
		},
		{
			name:      "over item limit",
			itemLimit: 3,
			methods:   []string{"debug_ping", "debug_ping", "debug_ping", "debug_ping"},
			wantErr:   true,
		},
		{
			name:       "within trace limit",
			itemLimit:  10,
			traceLimit: 2,
			methods:    []string{"debug_traceTransaction", "debug_traceBlockByNumber", "debug_ping"},
		},
		{
			name:       "over trace limit",
			itemLimit:  10,
			traceLimit: 2,
			methods:    []string{"debug_traceTransaction", "debug_traceBlockByNumber", "debug_traceTransaction"},
			wantErr:    true,
		},
		{
			name:       "non-trace calls do not count",
			itemLimit:  10,
			traceLimit: 1,
			methods:    []string{"debug_traceTransaction", "debug_ping", "debug_ping", "debug_ping"},
		},
		{
			name:    "limits disabled",
			methods: []string{"debug_ping", "debug_ping", "debug_ping", "debug_ping", "debug_ping"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := new(batchService)
			server := NewServer()
			server.SetBatchLimits(tt.itemLimit, tt.traceLimit)
			if err := server.RegisterName(DebugNamespace, service); err != nil {
				t.Fatal(err)
			}
			client := DialInProc(server)
			defer client.Close()

			batch := make([]BatchElem, len(tt.methods))
			for i, method := range tt.methods {
				batch[i] = BatchElem{Method: method, Result: new(string)}
			}
			if err := client.BatchCallContext(context.Background(), batch); err != nil {
				t.Fatalf("batch call failed: %v", err)
			}

			if tt.wantErr {
				// Every element is answered, so the caller does not block, and
				// nothing is executed.
				for _, elem := range batch {
					if elem.Error == nil {
						t.Fatalf("%s was executed in an over-limit batch", elem.Method)
					}
					if code := elem.Error.(Error).ErrorCode(); code != -32600 {
						t.Fatalf("%s: code = %d, want -32600", elem.Method, code)
					}
				}
				if service.calls != 0 {
					t.Fatalf("%d calls executed for a rejected batch", service.calls)
				}
				return
			}
			for _, elem := range batch {
				if elem.Error != nil {
					t.Fatalf("%s: %v", elem.Method, elem.Error)
				}
			}
			if service.calls != len(tt.methods) {
				t.Fatalf("executed %d calls, want %d", service.calls, len(tt.methods))
			}
		})
	}
}

func TestRejectedBatchResponseIDs(t *testing.T) {
	server := NewServer()
	server.SetBatchLimits(1, 0)
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	// The first two elements are valid notifications and must not receive a
	// response. Malformed elements without a usable id must receive id:null,
	// while the valid request must retain its id.
	body := `[
		{"jsonrpc":"2.0","method":"rpc_modules"},
		{"jsonrpc":"2.0","method":"eth_subscribe"},
		{"jsonrpc":"1.0","method":"rpc_modules"},
		{"jsonrpc":"2.0","method":""},
		{"jsonrpc":"2.0","id":1,"method":"rpc_modules"},
		{"jsonrpc":"2.0","id":[],"method":"rpc_modules"}
	]`
	resp, err := http.Post(httpServer.URL, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var messages []*jsonrpcMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatalf("invalid response %s: %v", data, err)
	}
	if len(messages) != 4 {
		t.Fatalf("response count = %d, want 4: %s", len(messages), data)
	}
	wantIDs := []string{"null", "null", "1", "null"}
	for i, message := range messages {
		if got := string(message.ID); got != wantIDs[i] {
			t.Errorf("response %d id = %s, want %s", i, got, wantIDs[i])
		}
		if message.Error == nil || message.Error.Code != -32600 {
			t.Errorf("response %d error = %#v, want -32600", i, message.Error)
		}
	}
}
