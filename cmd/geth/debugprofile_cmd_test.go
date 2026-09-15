// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
)

// The debug-profile flags are wired in cmd/utils/flags.go and validated in
// node.Config before any listener opens. The tests in node/ and rpc/ exercise
// node.Config and rpc.Server directly, which leaves the flag layer itself —
// flag names, defaults, IsSet plumbing, and the Fatalf paths — untested. These
// tests drive the real binary so that layer is covered end to end, and they
// assert the operator-facing message text rather than only that startup failed.

// debugProfileArgs builds an argument list for an inert node: it joins no
// network, opens no IPC socket, and runs a trivial console expression so the
// process exits on its own once startup has succeeded.
func debugProfileArgs(extra ...string) []string {
	args := []string{
		"--networkid", "1337",
		"--syncmode", "full",
		"--cache", "16",
		"--maxpeers", "0",
		"--port", "0",
		"--authrpc.port", "0",
		"--nodiscover",
		"--nat", "none",
		"--ipcdisable",
	}
	args = append(args, extra...)
	return append(args, "--exec", "1+1", "console")
}

// TestDebugProfileFatalStartup covers every configuration the operator guide
// documents as a fatal error. Each case asserts the exact message, a non-zero
// exit, and that no listener was ever opened — the "never briefly exposed"
// property is the whole point of validating before startRPC binds a port.
func TestDebugProfileFatalStartup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "http debug without profile",
			args: []string{"--http", "--http.port", "0", "--http.api", "eth,debug"},
			want: `the "debug" namespace is exposed on --http.api without --http.debug-profile`,
		},
		{
			name: "http both flags",
			args: []string{
				"--http", "--http.port", "0", "--http.api", "eth,debug",
				"--http.debug-profile", "trace-indexer-v1",
				"--http.allow-unsafe-debug",
			},
			want: "--http.debug-profile and --http.allow-unsafe-debug are mutually exclusive",
		},
		{
			name: "http profile without debug in api",
			args: []string{
				"--http", "--http.port", "0", "--http.api", "eth",
				"--http.debug-profile", "trace-indexer-v1",
			},
			want: `--http.debug-profile is set but "debug" is not in --http.api`,
		},
		{
			name: "http empty api",
			args: []string{"--http", "--http.port", "0", "--http.api", ""},
			want: `empty --http.api exposes every namespace, including "debug"`,
		},
		{
			name: "http unknown profile",
			args: []string{
				"--http", "--http.port", "0", "--http.api", "eth,debug",
				"--http.debug-profile", "trace-indexer-v2",
			},
			want: `unknown debug profile "trace-indexer-v2" (known profiles: trace-indexer-v1)`,
		},
		{
			name: "ws debug without profile",
			args: []string{"--ws", "--ws.port", "0", "--ws.api", "eth,debug"},
			want: `the "debug" namespace is exposed on --ws.api without --ws.debug-profile`,
		},
		{
			name: "ws both flags",
			args: []string{
				"--ws", "--ws.port", "0", "--ws.api", "eth,debug",
				"--ws.debug-profile", "trace-indexer-v1",
				"--ws.allow-unsafe-debug",
			},
			want: "--ws.debug-profile and --ws.allow-unsafe-debug are mutually exclusive",
		},
		{
			name: "ws profile without debug in api",
			args: []string{
				"--ws", "--ws.port", "0", "--ws.api", "eth",
				"--ws.debug-profile", "trace-indexer-v1",
			},
			want: `--ws.debug-profile is set but "debug" is not in --ws.api`,
		},
		{
			name: "ws empty api",
			args: []string{"--ws", "--ws.port", "0", "--ws.api", ""},
			want: `empty --ws.api exposes every namespace, including "debug"`,
		},
		{
			// Both transports misconfigured: HTTP is validated first, so the
			// operator is told about HTTP rather than being left to discover
			// the second failure on the next restart.
			name: "http reported before ws",
			args: []string{
				"--http", "--http.port", "0", "--http.api", "eth,debug",
				"--ws", "--ws.port", "0", "--ws.api", "eth,debug",
			},
			want: `the "debug" namespace is exposed on --http.api without --http.debug-profile`,
		},
		{
			// Rejected during flag parsing, before the node is even built:
			// both flags drive one shared limiter, so disagreeing values have
			// no meaning.
			name: "concurrency flags disagree",
			args: []string{
				"--http", "--http.port", "0", "--http.api", "eth,debug",
				"--http.debug-profile", "trace-indexer-v1",
				"--http.debug-trace.max-concurrency", "4",
				"--ws.debug-trace.max-concurrency", "8",
			},
			want: "--http.debug-trace.max-concurrency (4) and --ws.debug-trace.max-concurrency (8) configure the same shared limiter and must agree",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			geth := runGeth(t, debugProfileArgs(test.args...)...)
			// Startup failures are prefixed "Fatal: Error starting protocol
			// stack:" while flag-parsing failures are only "Fatal:", so match
			// the operator-facing message itself rather than the prefix.
			geth.ExpectRegexp(regexp.QuoteMeta(test.want))
			geth.WaitExit()

			if status := geth.ExitStatus(); status != 1 {
				t.Errorf("exit status = %d, want 1", status)
			}
			// Validation runs before startRPC binds anything. If a listener
			// log ever appears here, the surface was exposed — however
			// briefly — before the process gave up.
			if stderr := geth.StderrText(); strings.Contains(stderr, "HTTP server started") ||
				strings.Contains(stderr, "WebSocket enabled") {
				t.Errorf("a listener was opened before validation failed:\n%s", stderr)
			}
		})
	}
}

// TestDebugProfileStartupWarnings pins the startup warnings. The unsafe opt-in
// has to be loud exactly once, and a restricted transport has to be silent —
// a profile that warned would train operators to ignore the warning that
// matters.
func TestDebugProfileStartupWarnings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    string
		notWant []string
	}{
		{
			name: "unsafe opt-in warns",
			args: []string{
				"--http", "--http.port", "0", "--http.api", "eth,debug",
				"--http.allow-unsafe-debug",
			},
			want: "Full debug namespace exposed on untrusted transport",
		},
		{
			name: "empty api with unsafe opt-in warns about the module list",
			args: []string{
				"--http", "--http.port", "0", "--http.api", "",
				"--http.allow-unsafe-debug",
			},
			want: "Empty API module list exposes every namespace",
		},
		{
			name: "restricted transport is silent",
			args: []string{
				"--http", "--http.port", "0", "--http.api", "eth,debug",
				"--http.debug-profile", "trace-indexer-v1",
			},
			notWant: []string{
				"Full debug namespace exposed on untrusted transport",
				"Empty API module list exposes every namespace",
			},
		},
		{
			name: "ws unsafe opt-in warns",
			args: []string{
				"--ws", "--ws.port", "0", "--ws.api", "eth,debug",
				"--ws.allow-unsafe-debug",
			},
			want: "Full debug namespace exposed on untrusted transport",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			geth := runGeth(t, debugProfileArgs(test.args...)...)
			// The console evaluates the expression once the node is up, which
			// means startup completed and every warning has been emitted.
			geth.ExpectRegexp("2")
			geth.WaitExit()

			stderr := geth.StderrText()
			if test.want != "" {
				if got := strings.Count(stderr, test.want); got != 1 {
					t.Errorf("warning %q appeared %d times, want exactly 1:\n%s", test.want, got, stderr)
				}
			}
			for _, unwanted := range test.notWant {
				if strings.Contains(stderr, unwanted) {
					t.Errorf("unexpected warning %q:\n%s", unwanted, stderr)
				}
			}
		})
	}
}

// TestWSBatchLimitFlag drives --ws.rpc.batch-limit through the real binary.
//
// The verification script cannot reach this one: sending a genuine JSON-RPC
// batch over a WebSocket needs a real WS client, and geth's own console splits
// a batch into separate calls — it returns results for a batch the server
// would have rejected, so it would report success whatever the flag did. The
// HTTP side of the same flag is covered by scripts/test_debug_profile.sh.
func TestWSBatchLimitFlag(t *testing.T) {
	t.Parallel()

	const limit = 5
	port := freePort(t)
	endpoint := fmt.Sprintf("ws://127.0.0.1:%d", port)

	geth := runGeth(t,
		"--dev", "--port", "0", "--authrpc.port", "0",
		"--nodiscover", "--maxpeers", "0", "--ipcdisable",
		"--ws", "--ws.addr", "127.0.0.1", "--ws.port", strconv.Itoa(port),
		"--ws.origins", "*", "--ws.api", "eth,net,web3,debug",
		"--ws.debug-profile", "trace-indexer-v1",
		"--ws.rpc.batch-limit", strconv.Itoa(limit),
	)
	defer geth.Kill()
	waitForEndpoint(t, endpoint, 30*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := rpc.DialWebsocket(ctx, endpoint, "")
	if err != nil {
		t.Fatalf("dial %s: %v", endpoint, err)
	}
	defer client.Close()

	batchOf := func(n int) []rpc.BatchElem {
		batch := make([]rpc.BatchElem, n)
		for i := range batch {
			batch[i] = rpc.BatchElem{Method: "eth_chainId", Result: new(string)}
		}
		return batch
	}

	over := batchOf(limit + 1)
	if err := client.BatchCallContext(ctx, over); err != nil {
		t.Fatalf("batch call: %v", err)
	}
	want := fmt.Sprintf("batch too large: %d requests exceed the limit of %d", limit+1, limit)
	for i, elem := range over {
		if elem.Error == nil {
			t.Fatalf("element %d succeeded; the configured limit was not applied to WS", i)
		}
		var rpcErr rpc.Error
		if !errors.As(elem.Error, &rpcErr) {
			t.Fatalf("element %d: %v is not a JSON-RPC error", i, elem.Error)
		}
		if rpcErr.ErrorCode() != -32600 {
			t.Fatalf("element %d: code = %d, want -32600", i, rpcErr.ErrorCode())
		}
		if elem.Error.Error() != want {
			t.Fatalf("element %d: error = %q, want %q", i, elem.Error, want)
		}
	}

	atLimit := batchOf(limit)
	if err := client.BatchCallContext(ctx, atLimit); err != nil {
		t.Fatalf("batch call: %v", err)
	}
	for i, elem := range atLimit {
		if elem.Error != nil {
			t.Fatalf("element %d of an at-limit batch failed: %v", i, elem.Error)
		}
	}
}

// freePort reserves a port and hands it back. The gap between closing the
// listener and geth binding is a race in principle, but the alternative —
// a fixed port — collides with whatever else is on the machine.
func freePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
