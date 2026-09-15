// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package node

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/internal/testlog"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

// The restriction has to hold identically on both untrusted transports:
// restricting only HTTP would leave the whole surface open on the other port.
// Every other integration test in this package drives httpConfig, so these
// exercise the WebSocket path over a real connection instead.

// TraceChain gives the fake debug service a subscription, mirroring the real
// eth/tracers.API. A restricted transport must not register it — subscriptions
// are the one debug capability that cannot be reached over HTTP at all, which
// makes WS the only place the profile can be checked.
func (s *fakeDebugService) TraceChain(ctx context.Context) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return nil, rpc.ErrNotificationsUnsupported
	}
	return notifier.CreateSubscription(), nil
}

// startDebugWSServer starts a WebSocket-only RPC server carrying the fake debug
// service, restricted by the given profile options (nil for the full surface).
func startDebugWSServer(t *testing.T, debugProfile *rpc.RestrictedDebugOptions) string {
	t.Helper()

	srv := newHTTPServer(testlog.Logger(t, log.LvlDebug), rpc.DefaultHTTPTimeouts)
	config := wsConfig{
		Modules:      []string{"debug"},
		Origins:      []string{"*"},
		batchLimit:   DefaultBatchLimit,
		debugProfile: debugProfile,
	}
	apis := []rpc.API{{Namespace: "debug", Service: new(fakeDebugService)}}
	if err := srv.enableWS(apis, config); err != nil {
		t.Fatal(err)
	}
	if err := srv.setListenAddr("localhost", 0); err != nil {
		t.Fatal(err)
	}
	if err := srv.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.stop)
	return "ws://" + srv.listenAddr()
}

// dialWS opens a JSON-RPC client against a WebSocket endpoint.
func dialWS(t *testing.T, url string) *rpc.Client {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := rpc.DialWebsocket(ctx, url, "")
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(client.Close)
	return client
}

// errorCode extracts the JSON-RPC error code, failing the test if the error did
// not come back over the wire as a JSON-RPC error.
func errorCode(t *testing.T, err error) int {
	t.Helper()

	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error %v (%T) is not a JSON-RPC error", err, err)
	}
	return rpcErr.ErrorCode()
}

func TestRestrictedDebugWSTransport(t *testing.T) {
	url := startDebugWSServer(t, &rpc.RestrictedDebugOptions{Profile: rpc.DebugProfileTraceIndexerV1})
	client := dialWS(t, url)

	// The namespace stays visible over WS too: rpc_modules reports namespaces,
	// not per-method capabilities.
	modules, err := client.SupportedModules()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := modules["debug"]; !ok {
		t.Fatalf("debug namespace missing from rpc_modules: %v", modules)
	}

	for _, method := range []string{"debug_traceTransaction", "debug_traceBlockByNumber"} {
		var result string
		if err := client.Call(&result, method); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	for _, method := range []string{
		"debug_setHead", "debug_startCPUProfile", "debug_chaindbCompact",
		"debug_traceCall", "debug_traceBlockByHash", "debug_traceBlockFromFile",
	} {
		var result string
		err := client.Call(&result, method)
		if err == nil {
			t.Fatalf("%s is reachable on a restricted WS transport", method)
		}
		if code := errorCode(t, err); code != -32601 {
			t.Fatalf("%s: code = %d, want -32601", method, code)
		}
	}
}

func TestUnrestrictedDebugWSTransport(t *testing.T) {
	// Without a profile the full namespace is registered over WS, as before.
	client := dialWS(t, startDebugWSServer(t, nil))

	for _, method := range []string{
		"debug_traceTransaction", "debug_traceBlockByNumber", "debug_setHead",
		"debug_startCPUProfile", "debug_chaindbCompact", "debug_traceCall",
		"debug_traceBlockByHash", "debug_traceBlockFromFile",
	} {
		var result string
		if err := client.Call(&result, method); err != nil {
			t.Fatalf("%s missing on an unrestricted WS transport: %v", method, err)
		}
	}
}

// TestRestrictedDebugWSSubscribe covers the capability the HTTP tests
// structurally cannot: debug_subscribe. The restriction is not an explicit
// check but a consequence of the restricted service exporting no subscription
// methods, so it is worth pinning that the consequence actually holds.
func TestRestrictedDebugWSSubscribe(t *testing.T) {
	t.Run("restricted", func(t *testing.T) {
		client := dialWS(t, startDebugWSServer(t, &rpc.RestrictedDebugOptions{
			Profile: rpc.DebugProfileTraceIndexerV1,
		}))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		sub, err := client.Subscribe(ctx, "debug", make(chan interface{}), "traceChain")
		if err == nil {
			sub.Unsubscribe()
			t.Fatal("debug_subscribe traceChain is reachable on a restricted transport")
		}
		if code := errorCode(t, err); code != -32601 {
			t.Fatalf("code = %d, want -32601", code)
		}
		if want := `no "traceChain" subscription in debug namespace`; err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("unrestricted", func(t *testing.T) {
		client := dialWS(t, startDebugWSServer(t, nil))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		sub, err := client.Subscribe(ctx, "debug", make(chan interface{}), "traceChain")
		if err != nil {
			t.Fatalf("debug_subscribe traceChain unavailable without a profile: %v", err)
		}
		sub.Unsubscribe()
	})
}

// TestRestrictedDebugWSBatchLimits checks that both batch caps are enforced on
// WS. The limits live on the connection, and WS connections are long-lived, so
// a limit that only reached the HTTP handler would be invisible here.
func TestRestrictedDebugWSBatchLimits(t *testing.T) {
	url := startDebugWSServer(t, &rpc.RestrictedDebugOptions{Profile: rpc.DebugProfileTraceIndexerV1})
	client := dialWS(t, url)

	batchOf := func(n int, method string) []rpc.BatchElem {
		batch := make([]rpc.BatchElem, n)
		for i := range batch {
			batch[i] = rpc.BatchElem{Method: method, Result: new(string)}
		}
		return batch
	}
	callBatch := func(t *testing.T, batch []rpc.BatchElem) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.BatchCallContext(ctx, batch); err != nil {
			t.Fatalf("batch call: %v", err)
		}
	}

	t.Run("trace limit", func(t *testing.T) {
		batch := batchOf(rpc.MaxTraceCallsPerBatch+1, "debug_traceTransaction")
		callBatch(t, batch)

		want := "too many trace calls in batch: 11 exceed the limit of 10"
		for i, elem := range batch {
			if elem.Error == nil {
				t.Fatalf("element %d succeeded, want the whole batch rejected", i)
			}
			if code := errorCode(t, elem.Error); code != -32600 {
				t.Fatalf("element %d: code = %d, want -32600", i, code)
			}
			if elem.Error.Error() != want {
				t.Fatalf("element %d: error = %q, want %q", i, elem.Error, want)
			}
		}
	})

	t.Run("trace limit boundary", func(t *testing.T) {
		batch := batchOf(rpc.MaxTraceCallsPerBatch, "debug_traceTransaction")
		callBatch(t, batch)

		for i, elem := range batch {
			if elem.Error != nil {
				t.Fatalf("element %d of an at-limit batch failed: %v", i, elem.Error)
			}
		}
	})

	t.Run("item limit", func(t *testing.T) {
		// Non-trace calls do not count towards the trace cap, so this batch can
		// only be rejected by the element limit.
		batch := batchOf(DefaultBatchLimit+1, "rpc_modules")
		callBatch(t, batch)

		want := "batch too large: 101 requests exceed the limit of 100"
		for i, elem := range batch {
			if elem.Error == nil {
				t.Fatalf("element %d succeeded, want the whole batch rejected", i)
			}
			if elem.Error.Error() != want {
				t.Fatalf("element %d: error = %q, want %q", i, elem.Error, want)
			}
		}
	})
}
