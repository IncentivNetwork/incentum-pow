// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package node

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
)

// admin_startHTTP and admin_startWS can reopen a transport with a module list
// that startup never saw, which would bypass the boot-time validation entirely.
// The re-check added for that path is the only thing standing between an
// exposed admin namespace and a full debug surface, so it gets its own tests:
// nothing else in the suite calls these methods with debug in the module list.

// startDebugNode builds and starts a node carrying the fake debug service, with
// no transport enabled. Transports are opened by the admin API in each test.
func startDebugNode(t *testing.T, cfg *Config) (*Node, *adminAPI) {
	t.Helper()

	cfg.DataDir = ""
	stack, err := New(cfg)
	if err != nil {
		t.Fatal("can't create node:", err)
	}
	stack.RegisterAPIs([]rpc.API{{Namespace: "debug", Service: new(fakeDebugService)}})
	if err := stack.Start(); err != nil {
		t.Fatal("can't start node:", err)
	}
	t.Cleanup(func() { stack.Close() })
	return stack, &adminAPI{stack}
}

func TestAdminStartHTTPValidatesDebugProfile(t *testing.T) {
	t.Run("rejects debug without a profile", func(t *testing.T) {
		stack, api := startDebugNode(t, &Config{})

		ok, err := api.StartHTTP(sp("127.0.0.1"), ip(0), nil, sp("debug"), nil)
		if err == nil {
			t.Fatal("admin_startHTTP opened a transport with an unrestricted debug namespace")
		}
		if ok {
			t.Error("StartHTTP reported success alongside an error")
		}
		want := `the "debug" namespace is exposed on --http.api without --http.debug-profile`
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to contain %q", err, want)
		}
		// The transport must not have been opened despite the failure.
		if stack.http.listener != nil {
			t.Error("a listener was opened even though validation failed")
		}
	})

	t.Run("rejects a module list the node was not configured for", func(t *testing.T) {
		// The node boots with a profile, but the runtime call asks for the
		// unsafe opt-in as well. Startup never saw this combination.
		_, api := startDebugNode(t, &Config{
			HTTPDebugProfile:     rpc.DebugProfileTraceIndexerV1,
			HTTPAllowUnsafeDebug: true,
		})

		_, err := api.StartHTTP(sp("127.0.0.1"), ip(0), nil, sp("debug"), nil)
		if err == nil {
			t.Fatal("mutually exclusive flags were accepted at runtime")
		}
		if want := "mutually exclusive"; !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to contain %q", err, want)
		}
	})

	t.Run("accepts debug with a profile and restricts it", func(t *testing.T) {
		stack, api := startDebugNode(t, &Config{HTTPDebugProfile: rpc.DebugProfileTraceIndexerV1})

		if _, err := api.StartHTTP(sp("127.0.0.1"), ip(0), nil, sp("debug"), nil); err != nil {
			t.Fatal("admin_startHTTP refused a properly restricted transport:", err)
		}

		url := "http://" + stack.http.listenAddr()
		if resp := callDebug(t, url, "debug_traceTransaction"); resp.Error != nil {
			t.Fatalf("profile method unavailable: %v", resp.Error)
		}
		for _, method := range []string{"debug_setHead", "debug_chaindbCompact"} {
			resp := callDebug(t, url, method)
			if resp.Error == nil {
				t.Fatalf("%s is reachable on a transport opened at runtime", method)
			}
			if resp.Error.Code != -32601 {
				t.Fatalf("%s: code = %d, want -32601", method, resp.Error.Code)
			}
		}

		// The limiter protects node-wide resources, so a transport opened at
		// runtime has to share the node's limiter rather than get a fresh one
		// that would silently double the allowed concurrency.
		profile := stack.http.httpConfig.debugProfile
		if profile == nil {
			t.Fatal("restricted options missing from the runtime transport")
		}
		if profile.Limiter != stack.traceLimiter {
			t.Error("runtime transport uses its own trace limiter instead of the node's")
		}
	})
}

func TestAdminStartWSValidatesDebugProfile(t *testing.T) {
	t.Run("rejects debug without a profile", func(t *testing.T) {
		stack, api := startDebugNode(t, &Config{})

		ok, err := api.StartWS(sp("127.0.0.1"), ip(0), nil, sp("debug"))
		if err == nil {
			t.Fatal("admin_startWS opened a transport with an unrestricted debug namespace")
		}
		if ok {
			t.Error("StartWS reported success alongside an error")
		}
		want := `the "debug" namespace is exposed on --ws.api without --ws.debug-profile`
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to contain %q", err, want)
		}
		// Either server can carry WS depending on how the ports resolve, so a
		// listener on either one means the transport was opened.
		if stack.http.listener != nil || stack.ws.listener != nil {
			t.Error("a listener was opened even though validation failed")
		}
	})

	t.Run("accepts debug with a profile and restricts it", func(t *testing.T) {
		stack, api := startDebugNode(t, &Config{WSDebugProfile: rpc.DebugProfileTraceIndexerV1})

		if _, err := api.StartWS(sp("127.0.0.1"), ip(0), nil, sp("debug")); err != nil {
			t.Fatal("admin_startWS refused a properly restricted transport:", err)
		}

		// StartWS picks the server the same way, and with no HTTP host
		// configured that is the shared http server rather than n.ws.
		server := stack.wsServerForPort(0, false)
		client := dialWS(t, "ws://"+server.listenAddr())
		var result string
		if err := client.Call(&result, "debug_traceTransaction"); err != nil {
			t.Fatalf("profile method unavailable: %v", err)
		}
		err := client.Call(&result, "debug_setHead")
		if err == nil {
			t.Fatal("debug_setHead is reachable on a WS transport opened at runtime")
		}
		if code := errorCode(t, err); code != -32601 {
			t.Fatalf("debug_setHead: code = %d, want -32601", code)
		}

		profile := server.wsConfig.debugProfile
		if profile == nil {
			t.Fatal("restricted options missing from the runtime transport")
		}
		if profile.Limiter != stack.traceLimiter {
			t.Error("runtime transport uses its own trace limiter instead of the node's")
		}
	})
}
