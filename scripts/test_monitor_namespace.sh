#!/bin/bash
#
# test_monitor_namespace.sh — end-to-end verification of the read-only monitor
# RPC namespace against a real node.
#
# The namespace was added so an operator can serve node health without exposing
# admin. That is a claim about a module list as it is actually typed on the
# command line, so this drives the real binary: it starts nodes with the
# operator-facing flag sets and checks what each transport really serves.
#
# It is safe to run anywhere: every node it talks to is one it started a moment
# earlier, and both monitor methods are read-only.
#
# Usage:
#   ./scripts/test_monitor_namespace.sh              # run against a local binary
#   RUNNER=docker ./scripts/test_monitor_namespace.sh
#
# Environment:
#   RUNNER     binary (default) or docker
#   GETH       path to the geth binary (default: build/bin/geth, built if absent)
#   IMAGE      docker image to use   (default: incentum-pow:monitor)
#   DOCKER     docker command        (default: docker; use "sudo docker" if
#              your user is not in the docker group)
#   BASE_PORT  first host port to use (default: 18645)
#
# Requirements: curl, jq, and either a Go toolchain or docker.

set -uo pipefail

cd "$(dirname "$0")/.." || exit 2

RUNNER="${RUNNER:-binary}"
GETH="${GETH:-build/bin/geth}"
IMAGE="${IMAGE:-incentum-pow:monitor}"
DOCKER="${DOCKER:-docker}"
BASE_PORT="${BASE_PORT:-18645}"

WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/monitorns.XXXXXX")
# geth puts its IPC socket under the datadir, and a Unix socket path is capped
# at 107 characters. Past that the node dies at startup with "bind: invalid
# argument" and every phase fails for a reason unrelated to the namespace.
if [ ${#WORKDIR} -gt 60 ]; then
	rmdir "$WORKDIR" 2>/dev/null
	WORKDIR=$(mktemp -d /tmp/monitorns.XXXXXX)
	echo "note: TMPDIR is too long for an IPC socket; using $WORKDIR"
fi
NODE_PIDS=()
NODE_CONTAINERS=()

pass=0
fail=0
skip=0

ok() {
	printf '  \033[32mPASS\033[0m %s\n' "$1"
	pass=$((pass + 1))
}

bad() {
	printf '  \033[31mFAIL\033[0m %s\n' "$1"
	[ $# -gt 1 ] && printf '       %s\n' "$2"
	fail=$((fail + 1))
}

skipped() {
	printf '  \033[33mSKIP\033[0m %s\n' "$1"
	[ $# -gt 1 ] && printf '       %s\n' "$2"
	skip=$((skip + 1))
}

section() { printf '\n\033[1m--- %s ---\033[0m\n' "$1"; }

cleanup() {
	# Only ever kill processes this script started: a pattern-based pkill would
	# match the script's own command line.
	for pid in ${NODE_PIDS+"${NODE_PIDS[@]}"}; do
		kill "$pid" 2>/dev/null
		wait "$pid" 2>/dev/null
	done
	for name in ${NODE_CONTAINERS+"${NODE_CONTAINERS[@]}"}; do
		$DOCKER rm -f "$name" >/dev/null 2>&1
	done
	rm -rf "$WORKDIR"
}
trap cleanup EXIT

prepare_runner() {
	case "$RUNNER" in
	binary)
		if [ ! -x "$GETH" ]; then
			echo "building $GETH ..."
			make geth >/dev/null || {
				echo "make geth failed" >&2
				exit 2
			}
		fi
		GETH=$(cd "$(dirname "$GETH")" && pwd)/$(basename "$GETH")
		echo "runner: binary ($GETH)"
		;;
	docker)
		if ! $DOCKER image inspect "$IMAGE" >/dev/null 2>&1; then
			echo "building $IMAGE ..."
			$DOCKER build -t "$IMAGE" . >/dev/null || {
				echo "docker build failed" >&2
				exit 2
			}
		fi
		echo "runner: docker ($IMAGE)"
		;;
	*)
		echo "unknown RUNNER: $RUNNER (want binary or docker)" >&2
		exit 2
		;;
	esac
}

rpc() { # endpoint, payload
	curl -sS --max-time 30 -H 'Content-Type: application/json' --data "$2" "$1"
}

call() { # endpoint, method
	rpc "$1" "{\"jsonrpc\":\"2.0\",\"method\":\"$2\",\"params\":[],\"id\":1}"
}

call_with() { # endpoint, method, params-json
	rpc "$1" "{\"jsonrpc\":\"2.0\",\"method\":\"$2\",\"params\":$3,\"id\":1}"
}

# start_geth starts a node. $1 label, $2 host HTTP port, $3 host WS port (0 when
# WS is unused), $4 host p2p port, rest are geth flags — which must NOT include
# --http.port/--ws.port/--port: under docker the node listens on fixed ports
# inside the container that are published onto the host ones.
#
# An explicit p2p port matters here: without a listener p2p.Server never runs
# setupListening, and monitor_nodeInfo reports an empty listenAddr and port 0 —
# legitimately, but it would leave the fields this namespace exists to serve
# unasserted. Sets NODE_HTTP, NODE_WS, NODE_P2P, NODE_LOG, NODE_IPC.
start_geth() {
	local label="$1" http_port="$2" ws_port="$3" p2p_port="$4"
	shift 4

	local datadir="$WORKDIR/$label"
	mkdir -p "$datadir"
	NODE_LOG="$datadir/geth.log"
	NODE_HTTP="http://127.0.0.1:$http_port"
	NODE_WS=""
	[ "$ws_port" != 0 ] && NODE_WS="ws://127.0.0.1:$ws_port"
	NODE_P2P="$p2p_port"

	# NODE_MAXPEERS lets the peering phase raise the limit; every other phase
	# wants an isolated node.
	local inert=(--networkid 1337 --syncmode full --cache 16
		--maxpeers "${NODE_MAXPEERS:-0}" --authrpc.port 0 --nodiscover --nat none)

	if [ "$RUNNER" = docker ]; then
		local name="monitorns-$label"
		# Published ports are bound to loopback explicitly. Docker's default is
		# every host interface, and one of the nodes below is deliberately
		# started with admin in its module list — that control node must not be
		# reachable from outside this machine for the length of the run.
		local ports=(-p "127.0.0.1:$http_port:8545" -p "127.0.0.1:$p2p_port:$p2p_port")
		local listen=(--http.port 8545 --port "$p2p_port")
		if [ "$ws_port" != 0 ]; then
			ports+=(-p "127.0.0.1:$ws_port:8546")
			listen+=(--ws.port 8546)
		fi
		$DOCKER rm -f "$name" >/dev/null 2>&1
		$DOCKER run -d --name "$name" "${ports[@]}" "$IMAGE" \
			--datadir /data --http.addr 0.0.0.0 --http.vhosts '*' \
			--ws.addr 0.0.0.0 --ws.origins '*' \
			"${inert[@]}" "${listen[@]}" "$@" >/dev/null || return 1
		NODE_CONTAINERS+=("$name")
		NODE_IPC="$name:/data/geth.ipc"
	else
		local listen=(--http.port "$http_port" --port "$p2p_port")
		[ "$ws_port" != 0 ] && listen+=(--ws.port "$ws_port")
		"$GETH" --datadir "$datadir" "${inert[@]}" "${listen[@]}" "$@" >"$NODE_LOG" 2>&1 &
		NODE_PIDS+=($!)
		NODE_IPC="$datadir/geth.ipc"
	fi

	local waited=0
	while [ "$waited" -lt 60 ]; do
		if call "$NODE_HTTP" eth_chainId 2>/dev/null | grep -q result; then
			return 0
		fi
		sleep 0.5
		waited=$((waited + 1))
	done
	return 1
}

node_log() {
	if [ "$RUNNER" = docker ]; then
		[ ${#NODE_CONTAINERS[@]} -eq 0 ] && return
		$DOCKER logs "${NODE_CONTAINERS[-1]}" 2>&1
	else
		[ -f "$NODE_LOG" ] && cat "$NODE_LOG"
	fi
}

# attach runs a console expression against an endpoint. Dispatch is on the URL
# scheme: a substring test for ":/" would also match the "://" of a ws:// URL
# and mistake the scheme for a container name.
attach() {
	local endpoint="$1" expr="$2"

	if [ "$RUNNER" != docker ]; then
		"$GETH" attach --exec "$expr" "$endpoint" 2>&1 | tail -1
		return
	fi
	case "$endpoint" in
	ws://*) $DOCKER exec "${NODE_CONTAINERS[-1]}" geth attach --exec "$expr" "ws://127.0.0.1:8546" 2>&1 | tail -1 ;;
	http://*) $DOCKER exec "${NODE_CONTAINERS[-1]}" geth attach --exec "$expr" "http://127.0.0.1:8545" 2>&1 | tail -1 ;;
	*) $DOCKER exec "${endpoint%%:*}" geth attach --exec "$expr" "${endpoint#*:}" 2>&1 | tail -1 ;;
	esac
}

# rpc_over_console issues a single JSON-RPC call through the geth console. It is
# how the WS and IPC transports are reached: curl speaks neither usefully. Only
# single calls — the console splits a batch into separate requests.
rpc_over_console() { # endpoint, method
	attach "$1" "JSON.stringify(web3.currentProvider.send({jsonrpc:\"2.0\",method:\"$2\",params:[],id:1}))"
}

# expect_method_absent asserts a method is not registered on a transport.
expect_method_absent() { # label, endpoint, method
	local body code
	body=$(call "$2" "$3")
	code=$(printf '%s' "$body" | jq -r '.error.code // empty' 2>/dev/null)
	if [ "$code" = "-32601" ]; then
		ok "$1"
	else
		bad "$1" "code ${code:-none} — $body"
	fi
}

# ---------------------------------------------------------------------------
# Phase 1 — the namespace is opt-in and, once opted in, serves its payload
# ---------------------------------------------------------------------------

phase_optin() {
	section "Phase 1: opt-in and payload"

	local http=$BASE_PORT p2p=$((BASE_PORT + 100))
	if ! start_geth monitor "$http" 0 "$p2p" \
		--http --http.api eth,net,web3,txpool,monitor; then
		bad "monitor node did not start" "$(node_log | tail -3)"
		return
	fi
	local ep="$NODE_HTTP"

	local modules
	modules=$(call "$ep" rpc_modules | jq -r '.result // empty')
	if printf '%s' "$modules" | jq -e 'has("monitor")' >/dev/null 2>&1; then
		ok "rpc_modules advertises monitor"
	else
		bad "rpc_modules advertises monitor" "$modules"
	fi

	local info result
	info=$(call "$ep" monitor_nodeInfo)
	result=$(printf '%s' "$info" | jq -c '.result // empty')
	if [ -z "$result" ]; then
		bad "monitor_nodeInfo answers" "$info"
		return
	fi
	ok "monitor_nodeInfo answers"

	local field
	for field in name ip listenAddr network difficulty; do
		if [ -n "$(printf '%s' "$result" | jq -r --arg f "$field" '.[$f] // empty')" ]; then
			ok "nodeInfo.$field is populated"
		else
			bad "nodeInfo.$field is populated" "$result"
		fi
	done

	# The listener port is the one this script asked for, which is what makes
	# the field trustworthy: a placeholder would read 0.
	if [ "$(printf '%s' "$result" | jq -r '.ports.listener')" = "$NODE_P2P" ]; then
		ok "nodeInfo.ports.listener is the configured p2p port ($NODE_P2P)"
	else
		bad "nodeInfo.ports.listener is the configured p2p port" \
			"want $NODE_P2P, got $(printf '%s' "$result" | jq -r '.ports.listener')"
	fi
	# Discovery is off, so the field must be present and zero rather than absent.
	if [ "$(printf '%s' "$result" | jq -r '.ports | has("discovery")')" = "true" ]; then
		ok "nodeInfo.ports.discovery is present"
	else
		bad "nodeInfo.ports.discovery is present" "$result"
	fi

	# difficulty is a *big.Int and marshals as a bare number. Monitoring parses
	# it as one; a string would be a silent wire-contract change.
	if [ "$(printf '%s' "$result" | jq -r '.difficulty | type')" = number ]; then
		ok "nodeInfo.difficulty is a JSON number"
	else
		bad "nodeInfo.difficulty is a JSON number" \
			"type is $(printf '%s' "$result" | jq -r '.difficulty | type')"
	fi

	local peers
	peers=$(call "$ep" monitor_peerCount | jq -r 'if (.result | type) == "number" then .result else empty end')
	if [ "$peers" = "0" ]; then
		ok "monitor_peerCount is 0 on an isolated node"
	else
		bad "monitor_peerCount is 0 on an isolated node" "got ${peers:-a non-number}"
	fi

	MONITOR_EP="$ep"
}

# ---------------------------------------------------------------------------
# Phase 2 — admin is not dragged along
# ---------------------------------------------------------------------------

phase_admin_absent() {
	section "Phase 2: admin stays off the monitor transport"

	if [ -z "${MONITOR_EP:-}" ]; then
		skipped "admin is absent from a monitor transport" "the phase 1 node did not start"
		return
	fi

	local modules
	modules=$(call "$MONITOR_EP" rpc_modules | jq -r '.result // empty')
	if printf '%s' "$modules" | jq -e 'has("admin")' >/dev/null 2>&1; then
		bad "rpc_modules does not advertise admin" "$modules"
	else
		ok "rpc_modules does not advertise admin"
	fi

	local m
	# exportChain and importChain live on eth's AdminAPI rather than node's, but
	# they are registered under the same namespace and the operator guide names
	# them among the methods monitor exists to stop granting.
	for m in admin_nodeInfo admin_peers admin_datadir admin_addPeer \
		admin_removePeer admin_startHTTP admin_startWS admin_stopWS \
		admin_exportChain admin_importChain; do
		expect_method_absent "$m is not served" "$MONITOR_EP" "$m"
	done

	# The control. Without it the checks above would also pass on a node where
	# admin was broken for some unrelated reason, which would prove nothing
	# about the module list.
	local http=$((BASE_PORT + 1)) p2p=$((BASE_PORT + 101))
	if ! start_geth admin "$http" 0 "$p2p" \
		--http --http.api eth,net,web3,monitor,admin; then
		bad "control node did not start" "$(node_log | tail -3)"
		return
	fi
	if [ -n "$(call "$NODE_HTTP" admin_nodeInfo | jq -r '.result.id // empty')" ]; then
		ok "admin answers when it IS in the module list (the control)"
	else
		bad "admin answers when it IS in the module list (the control)" \
			"$(call "$NODE_HTTP" admin_nodeInfo)"
	fi
}

# ---------------------------------------------------------------------------
# Phase 3 — the namespace really is opt-in
# ---------------------------------------------------------------------------

phase_not_listed() {
	section "Phase 3: absent when not listed"

	local http=$((BASE_PORT + 2)) p2p=$((BASE_PORT + 102))
	if ! start_geth plain "$http" 0 "$p2p" \
		--http --http.api eth,net,web3; then
		bad "plain node did not start" "$(node_log | tail -3)"
		return
	fi
	local ep="$NODE_HTTP"

	if call "$ep" rpc_modules | jq -e '.result | has("monitor")' >/dev/null 2>&1; then
		bad "rpc_modules omits monitor when it is not listed" "$(call "$ep" rpc_modules)"
	else
		ok "rpc_modules omits monitor when it is not listed"
	fi
	expect_method_absent "monitor_nodeInfo is not served" "$ep" monitor_nodeInfo
	expect_method_absent "monitor_peerCount is not served" "$ep" monitor_peerCount
}

# ---------------------------------------------------------------------------
# Phase 4 — the other transports
# ---------------------------------------------------------------------------

phase_transports() {
	section "Phase 4: WS and IPC"

	local http=$((BASE_PORT + 3)) ws=$((BASE_PORT + 4)) p2p=$((BASE_PORT + 103))
	if ! start_geth transports "$http" "$ws" "$p2p" \
		--http --http.api eth,net,web3,monitor \
		--ws --ws.api eth,net,web3,monitor; then
		bad "transports node did not start" "$(node_log | tail -3)"
		return
	fi

	local got
	for m in monitor_nodeInfo monitor_peerCount; do
		got=$(rpc_over_console "$NODE_WS" "$m")
		if printf '%s' "$got" | grep -q '\\"result\\"'; then
			ok "$m works over WS"
		else
			bad "$m works over WS" "$got"
		fi
	done

	# IPC is registered from the same api() list and is not filtered by a module
	# list at all, so the namespace must be there regardless.
	for m in monitor_nodeInfo monitor_peerCount; do
		got=$(rpc_over_console "$NODE_IPC" "$m")
		if printf '%s' "$got" | grep -q '\\"result\\"'; then
			ok "$m works over IPC"
		else
			bad "$m works over IPC" "$got"
		fi
	done
}

# ---------------------------------------------------------------------------
# Phase 5 — the documented minimum-privilege flag set
# ---------------------------------------------------------------------------

phase_minimum_privilege() {
	section "Phase 5: monitor alongside a restricted debug namespace"

	local http=$((BASE_PORT + 5)) p2p=$((BASE_PORT + 105))
	if ! start_geth minprivilege "$http" 0 "$p2p" \
		--http --http.api eth,net,web3,txpool,monitor,debug \
		--http.debug-profile trace-indexer-v1; then
		bad "minimum-privilege node did not start" "$(node_log | tail -3)"
		return
	fi
	local ep="$NODE_HTTP"

	if [ -n "$(call "$ep" monitor_nodeInfo | jq -r '.result.name // empty')" ]; then
		ok "monitor works next to a restricted debug namespace"
	else
		bad "monitor works next to a restricted debug namespace" "$(call "$ep" monitor_nodeInfo)"
	fi
	expect_method_absent "admin is still absent" "$ep" admin_nodeInfo
	expect_method_absent "the debug profile still withholds debug_setHead" "$ep" debug_setHead
	if [ -n "$(call "$ep" rpc_modules | jq -r '.result.debug // empty')" ]; then
		ok "debug is advertised, restricted to the profile methods"
	else
		bad "debug is advertised" "$(call "$ep" rpc_modules)"
	fi
}

# ---------------------------------------------------------------------------
# Phase 6 — peerCount with a peer actually connected
#
# Every other phase runs on an isolated node, where the correct answer is 0.
# An implementation that ignored the p2p server and returned a constant 0 would
# satisfy all of them. This is the only check that distinguishes a real reading
# from a plausible-looking constant, so it connects two nodes and expects the
# count to move.
# ---------------------------------------------------------------------------

phase_peer_count() {
	section "Phase 6: peerCount with a connected peer"

	local a_http=$((BASE_PORT + 6)) a_p2p=$((BASE_PORT + 106))
	local b_http=$((BASE_PORT + 7)) b_p2p=$((BASE_PORT + 107))

	# admin is needed on both: one to read its enode, the other to dial it.
	# These are throwaway local nodes and, under docker, bound to loopback.
	NODE_MAXPEERS=5
	if ! start_geth peer_a "$a_http" 0 "$a_p2p" \
		--http --http.api eth,net,web3,monitor,admin; then
		bad "peer node A did not start" "$(node_log | tail -3)"
		NODE_MAXPEERS=0
		return
	fi
	local a_ep="$NODE_HTTP" a_container=""
	[ "$RUNNER" = docker ] && a_container="${NODE_CONTAINERS[-1]}"

	if ! start_geth peer_b "$b_http" 0 "$b_p2p" \
		--http --http.api eth,net,web3,monitor,admin; then
		bad "peer node B did not start" "$(node_log | tail -3)"
		NODE_MAXPEERS=0
		return
	fi
	local b_ep="$NODE_HTTP"
	NODE_MAXPEERS=0

	local enode
	enode=$(call "$a_ep" admin_nodeInfo | jq -r '.result.enode // empty')
	if [ -z "$enode" ]; then
		bad "monitor_peerCount rises to 1 once a peer connects" \
			"could not read node A's enode"
		return
	fi

	# --nat none makes a node advertise itself on 127.0.0.1. Between two
	# containers that address is each container's own loopback, so node B would
	# dial itself and the phase could never run. Point the enode at the address
	# B can actually reach.
	if [ "$RUNNER" = docker ]; then
		local a_ip
		a_ip=$($DOCKER inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$a_container" 2>/dev/null)
		if [ -z "$a_ip" ]; then
			bad "monitor_peerCount rises to 1 once a peer connects" \
				"could not determine the container address of node A"
			return
		fi
		enode=$(printf '%s' "$enode" | sed -E "s#@[^:]+:#@$a_ip:#")
	fi

	if [ "$(call_with "$b_ep" admin_addPeer "[\"$enode\"]" | jq -r '.result // empty')" != "true" ]; then
		bad "monitor_peerCount rises to 1 once a peer connects" \
			"node B refused the addPeer request"
		return
	fi

	# Whether the nodes peered is established from admin_peers, not from the
	# method under test. Polling monitor_peerCount itself would make an
	# implementation that always returns 0 indistinguishable from two nodes that
	# never connected — it would time out and be written off as an environment
	# problem, which is exactly the hole this phase exists to close.
	local waited=0 a_peers=-1 b_peers=-1
	while [ "$waited" -lt 60 ]; do
		a_peers=$(call "$a_ep" admin_peers | jq -r 'if (.result | type) == "array" then (.result | length) else -1 end')
		b_peers=$(call "$b_ep" admin_peers | jq -r 'if (.result | type) == "array" then (.result | length) else -1 end')
		[ "$a_peers" = 1 ] && [ "$b_peers" = 1 ] && break
		sleep 0.5
		waited=$((waited + 1))
	done

	if [ "$a_peers" != 1 ] || [ "$b_peers" != 1 ]; then
		# This is the only check that distinguishes a real peer count from a
		# constant 0, so it is not allowed to quietly not run. A SKIP here would
		# leave the run green with the interesting check never executed.
		bad "monitor_peerCount rises to 1 once a peer connects" \
			"the two nodes did not peer within 30s (admin_peers A=$a_peers B=$b_peers)"
		return
	fi

	# The connection exists. From here a wrong count is the method's fault.
	local a_count b_count
	a_count=$(call "$a_ep" monitor_peerCount | jq -r 'if (.result | type) == "number" then .result else -1 end')
	b_count=$(call "$b_ep" monitor_peerCount | jq -r 'if (.result | type) == "number" then .result else -1 end')

	# Exactly one, not merely non-zero: there is exactly one connection.
	if [ "$a_count" = 1 ]; then
		ok "monitor_peerCount is 1 on node A once peered"
	else
		bad "monitor_peerCount is 1 on node A once peered" \
			"admin_peers reports 1 connection, monitor_peerCount reports $a_count"
	fi
	if [ "$b_count" = 1 ]; then
		ok "monitor_peerCount is 1 on node B once peered"
	else
		bad "monitor_peerCount is 1 on node B once peered" \
			"admin_peers reports 1 connection, monitor_peerCount reports $b_count"
	fi

	# And the count follows the connection back down, which a constant cannot.
	if [ "$(call_with "$b_ep" admin_removePeer "[\"$enode\"]" | jq -r '.result // empty')" != "true" ]; then
		bad "monitor_peerCount falls back to 0 once the peer leaves" \
			"node B refused the removePeer request"
		return
	fi
	waited=0
	while [ "$waited" -lt 60 ]; do
		b_peers=$(call "$b_ep" admin_peers | jq -r 'if (.result | type) == "array" then (.result | length) else -1 end')
		[ "$b_peers" = 0 ] && break
		sleep 0.5
		waited=$((waited + 1))
	done
	if [ "$b_peers" != 0 ]; then
		bad "monitor_peerCount falls back to 0 once the peer leaves" \
			"the connection was still up after 30s (admin_peers=$b_peers)"
		return
	fi
	b_count=$(call "$b_ep" monitor_peerCount | jq -r 'if (.result | type) == "number" then .result else -1 end')
	if [ "$b_count" = 0 ]; then
		ok "monitor_peerCount falls back to 0 once the peer leaves"
	else
		bad "monitor_peerCount falls back to 0 once the peer leaves" \
			"admin_peers reports no connections, monitor_peerCount reports $b_count"
	fi
}

# ---------------------------------------------------------------------------

echo "=== monitor namespace verification ==="
prepare_runner

phase_optin
phase_admin_absent
phase_not_listed
phase_transports
phase_minimum_privilege
phase_peer_count

# The stopped-node behaviour of both methods (ErrNodeStopped) cannot be reached
# over a transport: the transport is only open while the node is running.
skipped "monitor methods report ErrNodeStopped on a stopped node" \
	"not reachable over a transport; covered by node TestMonitorNodeInfoStoppedNode and TestMonitorPeerCountStoppedNode"

printf '\n=== %d passed, %d failed, %d skipped ===\n' "$pass" "$fail" "$skip"
[ "$fail" -eq 0 ] || exit 1
