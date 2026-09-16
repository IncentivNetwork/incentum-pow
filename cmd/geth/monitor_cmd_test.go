// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
)

// The monitor namespace exists so an operator can drop admin from an untrusted
// transport. Everything below drives the real binary with the operator-facing
// flag set, because that is the layer the promise is made at: the node tests
// construct node.Config directly and never exercise a module list as it is
// actually typed.

// monitorTestPort reserves a port and hands it back. Named distinctly from
// helpers in other test files in this package so the two can coexist.
func monitorTestPort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// startMonitorNode boots an inert node with the given API module lists. The
// node is killed when the test ends; callers dial it with dialMonitor.
//
// --dev is deliberately not used: it disables networking altogether, so
// p2p.Server never starts a listener and monitor_nodeInfo reports an empty
// listenAddr — legitimately, but it would leave the field unasserted. An
// explicit p2p port makes the listener real and the field meaningful.
func startMonitorNode(t *testing.T, extra ...string) {
	t.Helper()

	args := append([]string{
		"--networkid", "1337", "--syncmode", "full", "--cache", "16",
		"--maxpeers", "0", "--port", fmt.Sprint(monitorTestPort(t)),
		"--authrpc.port", "0", "--nodiscover", "--nat", "none", "--ipcdisable",
	}, extra...)

	geth := runGeth(t, args...)
	t.Cleanup(geth.Kill)
}

// dialMonitor waits for an endpoint and connects to it.
func dialMonitor(t *testing.T, endpoint string) *rpc.Client {
	t.Helper()

	waitForEndpoint(t, endpoint, 30*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatalf("dial %s: %v", endpoint, err)
	}
	t.Cleanup(client.Close)
	return client
}

// monitorNodeInfoResult mirrors the documented shape of monitor_nodeInfo.
// Difficulty is kept raw so its JSON form can be asserted: *big.Int marshals as
// a bare number, and a monitoring stack parsing it as one would break if that
// ever became a string.
type monitorNodeInfoResult struct {
	Name       string `json:"name"`
	IP         string `json:"ip"`
	ListenAddr string `json:"listenAddr"`
	Ports      struct {
		Listener  int `json:"listener"`
		Discovery int `json:"discovery"`
	} `json:"ports"`
	Network    uint64          `json:"network"`
	Difficulty json.RawMessage `json:"difficulty"`
}

// assertMonitorWorks runs the two methods and checks the payload an operator's
// monitoring actually consumes.
func assertMonitorWorks(t *testing.T, client *rpc.Client) {
	t.Helper()

	var info monitorNodeInfoResult
	if err := client.Call(&info, "monitor_nodeInfo"); err != nil {
		t.Fatalf("monitor_nodeInfo: %v", err)
	}
	if info.Name == "" {
		t.Error("monitor_nodeInfo returned an empty name")
	}
	if info.ListenAddr == "" {
		t.Error("monitor_nodeInfo returned an empty listenAddr")
	}
	if info.Network == 0 {
		t.Error("monitor_nodeInfo returned network 0; the eth protocol branch did not run")
	}
	// A quoted difficulty would still decode into most consumers but would
	// change the wire contract this namespace was added to provide.
	if len(info.Difficulty) == 0 {
		t.Error("monitor_nodeInfo returned no difficulty")
	} else if info.Difficulty[0] == '"' {
		t.Errorf("difficulty is a JSON string (%s), want a number", info.Difficulty)
	}

	var peers int
	if err := client.Call(&peers, "monitor_peerCount"); err != nil {
		t.Fatalf("monitor_peerCount: %v", err)
	}
	if peers != 0 {
		t.Errorf("monitor_peerCount = %d, want 0 on an isolated node", peers)
	}
}

// assertAdminAbsent is the security half: exposing monitor must not drag the
// destructive namespace along with it.
func assertAdminAbsent(t *testing.T, client *rpc.Client) {
	t.Helper()

	for _, method := range []string{
		"admin_nodeInfo", "admin_peers", "admin_datadir",
		"admin_addPeer", "admin_removePeer", "admin_startHTTP", "admin_startWS",
	} {
		err := client.Call(new(json.RawMessage), method)
		if err == nil {
			t.Errorf("%s is reachable on a monitor-only transport", method)
			continue
		}
		var rpcErr rpc.Error
		if !errors.As(err, &rpcErr) {
			t.Errorf("%s: %v is not a JSON-RPC error", method, err)
			continue
		}
		if rpcErr.ErrorCode() != -32601 {
			t.Errorf("%s: code = %d, want -32601", method, rpcErr.ErrorCode())
		}
	}
}

func TestMonitorNamespaceOverHTTPFlag(t *testing.T) {
	t.Parallel()

	port := monitorTestPort(t)
	startMonitorNode(t,
		"--http", "--http.addr", "127.0.0.1", "--http.port", fmt.Sprint(port),
		"--http.api", "eth,net,web3,txpool,monitor",
	)
	client := dialMonitor(t, fmt.Sprintf("http://127.0.0.1:%d", port))

	modules, err := client.SupportedModules()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := modules["monitor"]; !ok {
		t.Fatalf("monitor missing from rpc_modules: %v", modules)
	}
	if _, ok := modules["admin"]; ok {
		t.Fatalf("admin advertised on a monitor-only transport: %v", modules)
	}

	assertMonitorWorks(t, client)
	assertAdminAbsent(t, client)
}

func TestMonitorNamespaceOverWSFlag(t *testing.T) {
	t.Parallel()

	port := monitorTestPort(t)
	startMonitorNode(t,
		"--ws", "--ws.addr", "127.0.0.1", "--ws.port", fmt.Sprint(port),
		"--ws.origins", "*", "--ws.api", "eth,net,web3,monitor",
	)
	client := dialMonitor(t, fmt.Sprintf("ws://127.0.0.1:%d", port))

	assertMonitorWorks(t, client)
	assertAdminAbsent(t, client)
}

// TestMonitorNamespaceIsOptIn covers the other direction. The namespace is
// registered unconditionally in node.apis(), so the only thing keeping it off a
// transport is the module list.
func TestMonitorNamespaceIsOptIn(t *testing.T) {
	t.Parallel()

	port := monitorTestPort(t)
	startMonitorNode(t,
		"--http", "--http.addr", "127.0.0.1", "--http.port", fmt.Sprint(port),
		"--http.api", "eth,net,web3",
	)
	client := dialMonitor(t, fmt.Sprintf("http://127.0.0.1:%d", port))

	for _, method := range []string{"monitor_nodeInfo", "monitor_peerCount"} {
		err := client.Call(new(json.RawMessage), method)
		if err == nil {
			t.Fatalf("%s is served without monitor in the module list", method)
		}
		var rpcErr rpc.Error
		if !errors.As(err, &rpcErr) || rpcErr.ErrorCode() != -32601 {
			t.Fatalf("%s: %v, want -32601", method, err)
		}
	}
}

// TestMonitorWithRestrictedDebug covers the combination the operator guide
// recommends: monitoring and indexer tracing on the same untrusted transport,
// with admin nowhere. The two features were added separately and nothing
// asserts they compose.
func TestMonitorWithRestrictedDebug(t *testing.T) {
	t.Parallel()

	port := monitorTestPort(t)
	startMonitorNode(t,
		"--http", "--http.addr", "127.0.0.1", "--http.port", fmt.Sprint(port),
		"--http.api", "eth,net,web3,txpool,monitor,debug",
		"--http.debug-profile", "trace-indexer-v1",
	)
	client := dialMonitor(t, fmt.Sprintf("http://127.0.0.1:%d", port))

	assertMonitorWorks(t, client)
	assertAdminAbsent(t, client)

	// The debug profile still restricts its own namespace alongside monitor.
	err := client.Call(new(json.RawMessage), "debug_setHead", "0x0")
	if err == nil {
		t.Fatal("debug_setHead is reachable next to the monitor namespace")
	}
	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.ErrorCode() != -32601 {
		t.Fatalf("debug_setHead: %v, want -32601", err)
	}
	if !strings.Contains(err.Error(), "does not exist/is not available") {
		t.Fatalf("debug_setHead: unexpected message %q", err)
	}
}
