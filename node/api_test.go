// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package node

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
)

// This test uses the admin_startRPC and admin_startWS APIs,
// checking whether the HTTP server is started correctly.
func TestStartRPC(t *testing.T) {
	type test struct {
		name string
		cfg  Config
		fn   func(*testing.T, *Node, *adminAPI)

		// Checks. These run after the node is configured and all API calls have been made.
		wantReachable bool // whether the HTTP server should be reachable at all
		wantHandlers  bool // whether RegisterHandler handlers should be accessible
		wantRPC       bool // whether JSON-RPC/HTTP should be accessible
		wantWS        bool // whether JSON-RPC/WS should be accessible
	}

	tests := []test{
		{
			name: "all off",
			cfg:  Config{},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
			},
			wantReachable: false,
			wantHandlers:  false,
			wantRPC:       false,
			wantWS:        false,
		},
		{
			name: "rpc enabled through config",
			cfg:  Config{HTTPHost: "127.0.0.1"},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
			},
			wantReachable: true,
			wantHandlers:  true,
			wantRPC:       true,
			wantWS:        false,
		},
		{
			name: "rpc enabled through API",
			cfg:  Config{},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				_, err := api.StartHTTP(sp("127.0.0.1"), ip(0), nil, nil, nil)
				assert.NoError(t, err)
			},
			wantReachable: true,
			wantHandlers:  true,
			wantRPC:       true,
			wantWS:        false,
		},
		{
			name: "rpc start again after failure",
			cfg:  Config{},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				// Listen on a random port.
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal("can't listen:", err)
				}
				defer listener.Close()
				port := listener.Addr().(*net.TCPAddr).Port

				// Now try to start RPC on that port. This should fail.
				_, err = api.StartHTTP(sp("127.0.0.1"), ip(port), nil, nil, nil)
				if err == nil {
					t.Fatal("StartHTTP should have failed on port", port)
				}

				// Try again after unblocking the port. It should work this time.
				listener.Close()
				_, err = api.StartHTTP(sp("127.0.0.1"), ip(port), nil, nil, nil)
				assert.NoError(t, err)
			},
			wantReachable: true,
			wantHandlers:  true,
			wantRPC:       true,
			wantWS:        false,
		},
		{
			name: "rpc stopped through API",
			cfg:  Config{HTTPHost: "127.0.0.1"},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				_, err := api.StopHTTP()
				assert.NoError(t, err)
			},
			wantReachable: false,
			wantHandlers:  false,
			wantRPC:       false,
			wantWS:        false,
		},
		{
			name: "rpc stopped twice",
			cfg:  Config{HTTPHost: "127.0.0.1"},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				_, err := api.StopHTTP()
				assert.NoError(t, err)

				_, err = api.StopHTTP()
				assert.NoError(t, err)
			},
			wantReachable: false,
			wantHandlers:  false,
			wantRPC:       false,
			wantWS:        false,
		},
		{
			name:          "ws enabled through config",
			cfg:           Config{WSHost: "127.0.0.1"},
			wantReachable: true,
			wantHandlers:  false,
			wantRPC:       false,
			wantWS:        true,
		},
		{
			name: "ws enabled through API",
			cfg:  Config{},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				_, err := api.StartWS(sp("127.0.0.1"), ip(0), nil, nil)
				assert.NoError(t, err)
			},
			wantReachable: true,
			wantHandlers:  false,
			wantRPC:       false,
			wantWS:        true,
		},
		{
			name: "ws stopped through API",
			cfg:  Config{WSHost: "127.0.0.1"},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				_, err := api.StopWS()
				assert.NoError(t, err)
			},
			wantReachable: false,
			wantHandlers:  false,
			wantRPC:       false,
			wantWS:        false,
		},
		{
			name: "ws stopped twice",
			cfg:  Config{WSHost: "127.0.0.1"},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				_, err := api.StopWS()
				assert.NoError(t, err)

				_, err = api.StopWS()
				assert.NoError(t, err)
			},
			wantReachable: false,
			wantHandlers:  false,
			wantRPC:       false,
			wantWS:        false,
		},
		{
			name: "ws enabled after RPC",
			cfg:  Config{HTTPHost: "127.0.0.1"},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				wsport := n.http.port
				_, err := api.StartWS(sp("127.0.0.1"), ip(wsport), nil, nil)
				assert.NoError(t, err)
			},
			wantReachable: true,
			wantHandlers:  true,
			wantRPC:       true,
			wantWS:        true,
		},
		{
			name: "ws enabled after RPC then stopped",
			cfg:  Config{HTTPHost: "127.0.0.1"},
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				wsport := n.http.port
				_, err := api.StartWS(sp("127.0.0.1"), ip(wsport), nil, nil)
				assert.NoError(t, err)

				_, err = api.StopWS()
				assert.NoError(t, err)
			},
			wantReachable: true,
			wantHandlers:  true,
			wantRPC:       true,
			wantWS:        false,
		},
		{
			name: "rpc stopped with ws enabled",
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				_, err := api.StartHTTP(sp("127.0.0.1"), ip(0), nil, nil, nil)
				assert.NoError(t, err)

				wsport := n.http.port
				_, err = api.StartWS(sp("127.0.0.1"), ip(wsport), nil, nil)
				assert.NoError(t, err)

				_, err = api.StopHTTP()
				assert.NoError(t, err)
			},
			wantReachable: false,
			wantHandlers:  false,
			wantRPC:       false,
			wantWS:        false,
		},
		{
			name: "rpc enabled after ws",
			fn: func(t *testing.T, n *Node, api *adminAPI) {
				_, err := api.StartWS(sp("127.0.0.1"), ip(0), nil, nil)
				assert.NoError(t, err)

				wsport := n.http.port
				_, err = api.StartHTTP(sp("127.0.0.1"), ip(wsport), nil, nil, nil)
				assert.NoError(t, err)
			},
			wantReachable: true,
			wantHandlers:  true,
			wantRPC:       true,
			wantWS:        true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Apply some sane defaults.
			config := test.cfg
			config.HTTPModules = []string{"web3"}
			config.WSModules = []string{"web3"}
			// config.Logger = testlog.Logger(t, log.LvlDebug)
			config.P2P.NoDiscovery = true
			if config.HTTPTimeouts == (rpc.HTTPTimeouts{}) {
				config.HTTPTimeouts = rpc.DefaultHTTPTimeouts
			}

			// Create Node.
			stack, err := New(&config)
			if err != nil {
				t.Fatal("can't create node:", err)
			}
			defer stack.Close()

			// Register the test handler.
			stack.RegisterHandler("test", "/test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("OK"))
			}))

			if err := stack.Start(); err != nil {
				t.Fatal("can't start node:", err)
			}

			// Run the API call hook.
			if test.fn != nil {
				test.fn(t, stack, &adminAPI{stack})
			}

			// Check if the HTTP endpoints are available.
			baseURL := stack.HTTPEndpoint()
			reachable := checkReachable(baseURL)
			handlersAvailable := checkBodyOK(baseURL + "/test")
			rpcAvailable := checkRPC(baseURL)
			wsAvailable := checkRPC(strings.Replace(baseURL, "http://", "ws://", 1))
			if reachable != test.wantReachable {
				t.Errorf("HTTP server is %sreachable, want it %sreachable", not(reachable), not(test.wantReachable))
			}
			if handlersAvailable != test.wantHandlers {
				t.Errorf("RegisterHandler handlers %savailable, want them %savailable", not(handlersAvailable), not(test.wantHandlers))
			}
			if rpcAvailable != test.wantRPC {
				t.Errorf("HTTP RPC %savailable, want it %savailable", not(rpcAvailable), not(test.wantRPC))
			}
			if wsAvailable != test.wantWS {
				t.Errorf("WS RPC %savailable, want it %savailable", not(wsAvailable), not(test.wantWS))
			}
		})
	}
}

// checkReachable checks if the TCP endpoint in rawurl is open.
func checkReachable(rawurl string) bool {
	u, err := url.Parse(rawurl)
	if err != nil {
		panic(err)
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// checkBodyOK checks whether the given HTTP URL responds with 200 OK and body "OK".
func checkBodyOK(url string) bool {
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return false
	}
	buf := make([]byte, 2)
	if _, err = io.ReadFull(resp.Body, buf); err != nil {
		return false
	}
	return bytes.Equal(buf, []byte("OK"))
}

// checkRPC checks whether JSON-RPC works against the given URL.
func checkRPC(url string) bool {
	c, err := rpc.Dial(url)
	if err != nil {
		return false
	}
	defer c.Close()

	_, err = c.SupportedModules()
	return err == nil
}

func TestMonitorNodeInfo(t *testing.T) {
	// An explicit ListenAddr is required so p2p.Server actually starts a
	// listener and reports a non-empty ListenAddr; without it the server
	// short-circuits setupListening and the field is legitimately empty.
	stack, err := New(&Config{P2P: p2p.Config{NoDiscovery: true, ListenAddr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()

	if err := stack.Start(); err != nil {
		t.Fatal(err)
	}

	api := &monitorAPI{stack}
	info, err := api.NodeInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Name == "" {
		t.Error("NodeInfo.Name is empty")
	}
	if info.ListenAddr == "" {
		t.Error("NodeInfo.ListenAddr is empty")
	}
}

func TestMonitorNodeInfoStoppedNode(t *testing.T) {
	stack, err := New(&Config{P2P: p2p.Config{NoDiscovery: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()

	api := &monitorAPI{stack}
	_, err = api.NodeInfo()
	if err != ErrNodeStopped {
		t.Errorf("expected ErrNodeStopped, got %v", err)
	}
}

func TestMonitorPeerCount(t *testing.T) {
	stack, err := New(&Config{P2P: p2p.Config{NoDiscovery: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()

	if err := stack.Start(); err != nil {
		t.Fatal(err)
	}

	api := &monitorAPI{stack}
	count, err := api.PeerCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 peers, got %d", count)
	}
}

func TestMonitorPeerCountStoppedNode(t *testing.T) {
	stack, err := New(&Config{P2P: p2p.Config{NoDiscovery: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()

	api := &monitorAPI{stack}
	_, err = api.PeerCount()
	if err != ErrNodeStopped {
		t.Errorf("expected ErrNodeStopped, got %v", err)
	}
}

// TestMonitorNamespaceOverHTTP exercises the two monitor methods over the
// actual HTTP JSON-RPC transport with only the `monitor` namespace registered.
// This is the acceptance-criteria integration test for issue #57: the methods
// must be reachable through a real transport with the operator-facing flag set,
// not just via direct Go calls.
func TestMonitorNamespaceOverHTTP(t *testing.T) {
	stack, err := New(&Config{
		HTTPHost:     "127.0.0.1",
		HTTPPort:     0,
		HTTPModules:  []string{"monitor"},
		HTTPTimeouts: rpc.DefaultHTTPTimeouts,
		P2P:          p2p.Config{NoDiscovery: true, ListenAddr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()

	if err := stack.Start(); err != nil {
		t.Fatal(err)
	}

	base := "http://" + stack.http.listenAddr()

	// monitor_nodeInfo must respond with a JSON object carrying the expected fields.
	resp := rpcRequest(t, base, "monitor_nodeInfo")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("monitor_nodeInfo status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{`"result"`, `"name"`, `"listenAddr"`, `"ports"`} {
		if !bytes.Contains(body, []byte(needle)) {
			t.Errorf("monitor_nodeInfo body missing %q; got %s", needle, body)
		}
	}

	// monitor_peerCount must respond with an integer result.
	resp = rpcRequest(t, base, "monitor_peerCount")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("monitor_peerCount status = %d, want 200", resp.StatusCode)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	// New node with no peers — expect result:0. Also assert no error field.
	if !bytes.Contains(body, []byte(`"result":0`)) {
		t.Errorf("monitor_peerCount body = %s, want result:0", body)
	}
	if bytes.Contains(body, []byte(`"error"`)) {
		t.Errorf("monitor_peerCount returned error: %s", body)
	}

	// admin methods must NOT be reachable — namespace is not registered.
	resp = rpcRequest(t, base, "admin_peers")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin_peers status = %d, want 200 (JSON-RPC method-not-found is a 200 with error body)", resp.StatusCode)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"error"`)) || !bytes.Contains(body, []byte("method")) {
		t.Errorf("admin_peers should have returned method-not-found; got %s", body)
	}
}

// string/int pointer helpers.
func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

func not(ok bool) string {
	if ok {
		return ""
	}
	return "not "
}
