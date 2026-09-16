#!/bin/bash
#
# test_debug_profile.sh — end-to-end verification of the fail-closed debug RPC
# profile (PR #119 / issue #118) against a real node.
#
# The Go tests cover node.Config and rpc.Server directly. This script covers
# what they cannot: the CLI flags as an operator types them, a real chain with
# real transactions to trace, the destructive methods themselves, and the IPC
# socket that must stay unrestricted. It is safe to run anywhere because the
# chain it attacks is one it created a moment earlier.
#
# Usage:
#   ./scripts/test_debug_profile.sh              # run against a local binary
#   RUNNER=docker ./scripts/test_debug_profile.sh
#
# Environment:
#   RUNNER     binary (default) or docker
#   GETH       path to the geth binary (default: build/bin/geth, built if absent)
#   IMAGE      docker image to use   (default: incentum-pow:debugprofile)
#   DOCKER     docker command        (default: docker; use "sudo docker" if
#              your user is not in the docker group)
#   BASE_PORT  first host port to use (default: 18545)
#
# Requirements: curl, jq, and either a Go toolchain or docker.

set -uo pipefail

for tool in curl jq; do
	command -v "$tool" >/dev/null 2>&1 || { echo "$tool is required" >&2; exit 2; }
done

cd "$(dirname "$0")/.." || exit 2

RUNNER="${RUNNER:-binary}"
GETH="${GETH:-build/bin/geth}"
IMAGE="${IMAGE:-incentum-pow:debugprofile}"
DOCKER="${DOCKER:-docker}"
BASE_PORT="${BASE_PORT:-18545}"

WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/debugprofile.XXXXXX")
# geth puts its IPC socket under the datadir, and a Unix socket path is capped
# at 107 characters. Past that the node dies at startup with "bind: invalid
# argument" — every phase then fails for a reason that has nothing to do with
# debug profiles. A long TMPDIR is enough to trigger it, so fall back to /tmp.
if [ ${#WORKDIR} -gt 60 ]; then
	rmdir "$WORKDIR" 2>/dev/null
	WORKDIR=$(mktemp -d /tmp/debugprofile.XXXXXX)
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
	# Only ever kill processes this script started: a pattern-based pkill here
	# would match the script's own command line.
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

# ---------------------------------------------------------------------------
# Runner abstraction. Both runners take the same geth flags so the matrix is
# written once.
# ---------------------------------------------------------------------------

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

# run_geth_once runs geth to completion, writing its combined output to
# $RUN_OUT and returning geth's exit code. Callers append "--exec <expr>
# console" so a node that starts successfully still exits. The output goes to a
# file rather than stdout because a command substitution would run this in a
# subshell, where the exit code could not be handed back.
RUN_OUT="$WORKDIR/run.out"
run_geth_once() {
	local datadir status
	datadir="$WORKDIR/once.$RANDOM"
	mkdir -p "$datadir"

	if [ "$RUNNER" = docker ]; then
		$DOCKER run --rm --network none "$IMAGE" --datadir /data "$@" >"$RUN_OUT" 2>&1
	else
		"$GETH" --datadir "$datadir" "$@" >"$RUN_OUT" 2>&1
	fi
	status=$?
	rm -rf "$datadir"
	return $status
}

# start_geth starts a long-running node. $1 is a label, $2 the host HTTP port,
# $3 the host WS port (0 when WS is not used), the rest are geth flags — which
# must NOT include --http.port/--ws.port: the container listens on its own fixed
# ports and is published onto the host ones, while the binary listens on the
# host ports directly. Sets NODE_HTTP, NODE_WS, NODE_LOG and NODE_IPC.
start_geth() {
	local label="$1" http_port="$2" ws_port="$3"
	shift 3

	local datadir="$WORKDIR/$label"
	mkdir -p "$datadir"
	NODE_LOG="$datadir/geth.log"
	NODE_HTTP="http://127.0.0.1:$http_port"
	NODE_WS=""
	[ "$ws_port" != 0 ] && NODE_WS="ws://127.0.0.1:$ws_port"

	# What the node itself sees, as opposed to what the host sees. Under docker
	# the two differ: the node listens on a fixed port inside the container,
	# published onto the host port, and must bind 0.0.0.0 for that publishing to
	# work at all. Anything asking the node to open a port of its own — such as
	# admin_startWS — has to speak in these terms, or it opens something the
	# host can never reach and the check that follows becomes meaningless.
	if [ "$RUNNER" = docker ]; then
		NODE_BIND_ADDR=0.0.0.0
		NODE_WS_INNER=8546
	else
		NODE_BIND_ADDR=127.0.0.1
		NODE_WS_INNER="$ws_port"
	fi

	if [ "$RUNNER" = docker ]; then
		local name="$(basename "$WORKDIR")-$label"
		local ports=(-p "127.0.0.1:$http_port:8545")
		local listen=(--http.port 8545)
		if [ "$ws_port" != 0 ]; then
			ports+=(-p "127.0.0.1:$ws_port:8546")
			listen+=(--ws.port 8546)
		fi
		$DOCKER run -d --name "$name" "${ports[@]}" "$IMAGE" \
			--datadir /data --http.addr 0.0.0.0 --http.vhosts '*' \
			--ws.addr 0.0.0.0 --ws.origins '*' "${listen[@]}" "$@" >/dev/null || return 1
		NODE_CONTAINERS+=("$name")
		NODE_IPC="$name:/data/geth.ipc"
	else
		local listen=(--http.port "$http_port")
		[ "$ws_port" != 0 ] && listen+=(--ws.port "$ws_port")
		"$GETH" --datadir "$datadir" "${listen[@]}" "$@" >"$NODE_LOG" 2>&1 &
		NODE_PIDS+=($!)
		NODE_IPC="$datadir/geth.ipc"
	fi

	# Wait for the endpoint to answer.
	local i
	for i in $(seq 1 60); do
		if rpc "$NODE_HTTP" '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' 2>/dev/null |
			grep -q result; then
			return 0
		fi
		sleep 0.5
	done
	return 1
}

node_log() {
	if [ "$RUNNER" = docker ]; then
		# start_geth appends to NODE_CONTAINERS only after a successful run, so
		# on the failure path this array can still be empty — the one case where
		# this function is needed most.
		[ ${#NODE_CONTAINERS[@]} -eq 0 ] && return
		$DOCKER logs "${NODE_CONTAINERS[-1]}" 2>&1
	else
		[ -f "$NODE_LOG" ] && cat "$NODE_LOG"
	fi
}

# attach runs a console expression against an endpoint. For docker the IPC path
# is inside the container, so the console runs there too.
attach() {
	local endpoint="$1" expr="$2"

	if [ "$RUNNER" != docker ]; then
		"$GETH" attach --exec "$expr" "$endpoint" 2>&1 | tail -1
		return
	fi

	# Under docker the console runs inside the container: the host may have no
	# geth binary at all, and the IPC socket exists only in the container.
	# Dispatch on the URL scheme — a substring test for ":/" would also match
	# the "://" of a ws:// endpoint and mistake "ws" for a container name.
	case "$endpoint" in
	ws://*)
		$DOCKER exec "${NODE_CONTAINERS[-1]}" geth attach --exec "$expr" \
			"ws://127.0.0.1:$NODE_WS_INNER" 2>&1 | tail -1
		;;
	http://*)
		$DOCKER exec "${NODE_CONTAINERS[-1]}" geth attach --exec "$expr" \
			"http://127.0.0.1:8545" 2>&1 | tail -1
		;;
	*)
		# <container>:/path/to/geth.ipc
		$DOCKER exec "${endpoint%%:*}" geth attach --exec "$expr" "${endpoint#*:}" 2>&1 | tail -1
		;;
	esac
}

# ---------------------------------------------------------------------------
# JSON-RPC helpers
# ---------------------------------------------------------------------------

rpc() { # endpoint, payload
	curl -sS --max-time 30 -H 'Content-Type: application/json' --data "$2" "$1"
}

expect_code() { # label, endpoint, payload, want_code, [want_msg]
	local label="$1" endpoint="$2" payload="$3" want="$4" want_msg="${5:-}"
	local body code msg

	body=$(rpc "$endpoint" "$payload")
	code=$(printf '%s' "$body" | jq -r '.error.code // empty' 2>/dev/null)
	msg=$(printf '%s' "$body" | jq -r '.error.message // empty' 2>/dev/null)
	if [ "$code" != "$want" ]; then
		bad "$label" "code ${code:-none}, want $want — $body"
		return
	fi
	if [ -n "$want_msg" ] && [ "$msg" != "$want_msg" ]; then
		bad "$label" "message: $msg
       want:    $want_msg"
		return
	fi
	ok "$label"
}

expect_ok() { # label, endpoint, payload
	local body
	body=$(rpc "$2" "$3")
	if printf '%s' "$body" | jq -e 'has("result")' >/dev/null 2>&1; then
		ok "$1"
	else
		bad "$1" "$body"
	fi
}

wait_receipt() { # endpoint, transaction hash
	local receipt i
	for ((i = 0; i < 60; i++)); do
		receipt=$(rpc "$1" "$(jq -nc --arg tx "$2" \
			'{jsonrpc:"2.0",method:"eth_getTransactionReceipt",params:[$tx],id:1}')")
		if printf '%s' "$receipt" | jq -e '.result.blockNumber != null' >/dev/null 2>&1; then
			printf '%s' "$receipt"
			return 0
		fi
		sleep 0.5
	done
	return 1
}

# build_batch renders a JSON-RPC batch of $1 elements from the printf template
# $2, which receives the element index as its id.
build_batch() { # count, template
	local i out=""
	for ((i = 0; i < $1; i++)); do
		[ -n "$out" ] && out="$out,"
		out="$out$(printf "$2" "$i")"
	done
	printf '[%s]' "$out"
}

# ---------------------------------------------------------------------------
# Phase 1 — configuration matrix
#
# Every one of these must be decided before a listener opens: a node that
# briefly serves the full namespace and then dies has still been exposed.
# ---------------------------------------------------------------------------

INERT=(--networkid 1337 --syncmode full --cache 16 --maxpeers 0
	--port 0 --authrpc.port 0 --nodiscover --nat none --ipcdisable)

matrix_fatal() { # label, want, flags...
	local label="$1" want="$2"
	shift 2

	if run_geth_once "${INERT[@]}" "$@" --exec '1+1' console; then
		bad "$label" "node started; expected a fatal error"
		return
	fi
	if ! grep -qF -- "$want" "$RUN_OUT"; then
		bad "$label" "missing expected message: $want
       got: $(grep -i fatal "$RUN_OUT" | head -1)"
		return
	fi
	if grep -qE 'HTTP server started|WebSocket enabled' "$RUN_OUT"; then
		bad "$label" "a listener opened before validation failed"
		return
	fi
	ok "$label"
}

matrix_starts() { # label, flags...
	local label="$1"
	shift
	MATRIX_OUT=""

	if ! run_geth_once "${INERT[@]}" "$@" --exec '1+1' console; then
		bad "$label" "$(grep -i fatal "$RUN_OUT" | head -1)"
		return
	fi
	MATRIX_OUT=$(cat "$RUN_OUT")
	ok "$label"
}

phase_matrix() {
	section "Phase 1: configuration matrix"

	matrix_fatal "http: debug without a profile is fatal" \
		'the "debug" namespace is exposed on --http.api without --http.debug-profile' \
		--http --http.port 0 --http.api eth,debug

	matrix_fatal "http: profile and unsafe opt-in are mutually exclusive" \
		'--http.debug-profile and --http.allow-unsafe-debug are mutually exclusive' \
		--http --http.port 0 --http.api eth,debug \
		--http.debug-profile trace-indexer-v1 --http.allow-unsafe-debug

	matrix_fatal "http: profile without debug in the module list is fatal" \
		'--http.debug-profile is set but "debug" is not in --http.api' \
		--http --http.port 0 --http.api eth --http.debug-profile trace-indexer-v1

	matrix_fatal "http: an empty module list is fatal" \
		'empty --http.api exposes every namespace, including "debug"' \
		--http --http.port 0 --http.api ''

	matrix_fatal "http: an unknown profile is fatal" \
		'unknown debug profile "trace-indexer-v2"' \
		--http --http.port 0 --http.api eth,debug --http.debug-profile trace-indexer-v2

	matrix_fatal "ws: debug without a profile is fatal" \
		'the "debug" namespace is exposed on --ws.api without --ws.debug-profile' \
		--ws --ws.port 0 --ws.api eth,debug

	matrix_fatal "ws: profile and unsafe opt-in are mutually exclusive" \
		'--ws.debug-profile and --ws.allow-unsafe-debug are mutually exclusive' \
		--ws --ws.port 0 --ws.api eth,debug \
		--ws.debug-profile trace-indexer-v1 --ws.allow-unsafe-debug

	matrix_fatal "ws: an empty module list is fatal" \
		'empty --ws.api exposes every namespace, including "debug"' \
		--ws --ws.port 0 --ws.api ''

	matrix_fatal "disagreeing concurrency flags are fatal" \
		'configure the same shared limiter and must agree' \
		--http --http.port 0 --http.api eth,debug --http.debug-profile trace-indexer-v1 \
		--http.debug-trace.max-concurrency 4 --ws.debug-trace.max-concurrency 8

	matrix_starts "a profiled transport starts" \
		--http --http.port 0 --http.api eth,debug --http.debug-profile trace-indexer-v1
	if printf '%s' "$MATRIX_OUT" | grep -qF 'Full debug namespace exposed on untrusted transport'; then
		bad "a profiled transport is silent" "it emitted the unsafe-debug warning"
	else
		ok "a profiled transport is silent"
	fi

	matrix_starts "the unsafe opt-in starts" \
		--http --http.port 0 --http.api eth,debug --http.allow-unsafe-debug
	local warns
	warns=$(printf '%s' "$MATRIX_OUT" | grep -cF 'Full debug namespace exposed on untrusted transport')
	if [ "$warns" = 1 ]; then
		ok "the unsafe opt-in warns exactly once"
	else
		bad "the unsafe opt-in warns exactly once" "warned $warns times"
	fi
}

# ---------------------------------------------------------------------------
# Phase 2 — a restricted node, end to end
# ---------------------------------------------------------------------------

phase_restricted() {
	section "Phase 2: restricted transport, end to end"

	local http_port=$BASE_PORT ws_port=$((BASE_PORT + 1))
	if ! start_geth restricted "$http_port" "$ws_port" \
		--dev --port 0 --authrpc.port 0 --nodiscover --maxpeers 0 \
		--http --http.api eth,net,web3,debug \
		--http.debug-profile trace-indexer-v1 \
		--ws --ws.api eth,net,web3,debug \
		--ws.debug-profile trace-indexer-v1; then
		bad "restricted node did not start" "$(node_log | tail -3)"
		return
	fi
	ok "restricted node started"

	local ep="$NODE_HTTP" ws="$NODE_WS"

	# -- a real transaction to trace ----------------------------------------
	local acc tx receipt trace contract block
	acc=$(rpc "$ep" '{"jsonrpc":"2.0","method":"eth_accounts","params":[],"id":1}' | jq -r '.result[0] // empty')
	if [ -z "$acc" ]; then
		bad "real trace" "no dev account available on the node created by this script"
		return
	else
		# Deploy a tiny contract that CALLs the identity precompile. A plain
		# transfer has no child calls and cannot prove onlyTopCall suppresses them.
		tx=$(rpc "$ep" "$(jq -nc --arg from "$acc" \
			'{jsonrpc:"2.0",method:"eth_sendTransaction",params:[{from:$from,gas:"0x20000",
			data:"0x6011600c60003960116000f360006000600060006000600461fffff15000"}],id:1}')" | jq -r '.result // empty')
		receipt=$(wait_receipt "$ep" "$tx")
		contract=$(printf '%s' "$receipt" | jq -r '.result.contractAddress // empty')
		if [ -z "$contract" ] || [ "$(printf '%s' "$receipt" | jq -r '.result.status')" != "0x1" ]; then
			bad "real trace" "test contract was not deployed"
			return
		fi
		tx=$(rpc "$ep" "$(jq -nc --arg from "$acc" --arg to "$contract" \
			'{jsonrpc:"2.0",method:"eth_sendTransaction",params:[{from:$from,to:$to,gas:"0x20000"}],id:1}')" | jq -r '.result // empty')
		receipt=$(wait_receipt "$ep" "$tx")
		block=$(printf '%s' "$receipt" | jq -r '.result.blockNumber // empty')
		if [ "$(printf '%s' "$receipt" | jq -r '.result.status // empty')" != "0x1" ]; then
			bad "real trace" "transaction was not mined successfully"
			return
		else
			# A registered method is not a working one: this is the only check
			# that the restricted wrapper actually delegates to the tracer.
			trace=$(rpc "$ep" "{\"jsonrpc\":\"2.0\",\"method\":\"debug_traceTransaction\",\"params\":[\"$tx\",{\"tracer\":\"callTracer\"}],\"id\":1}")
			if [ "$(printf '%s' "$trace" | jq -r '.result.type // empty')" = "CALL" ] &&
				[ "$(printf '%s' "$trace" | jq -r '.result.from // empty')" = "$acc" ] &&
				printf '%s' "$trace" | jq -e '.result.calls | length > 0' >/dev/null; then
				ok "debug_traceTransaction returns a real call trace"
			else
				bad "debug_traceTransaction returns a real call trace" "$trace"
			fi
			local full_trace="$trace"
			trace=$(rpc "$ep" "{\"jsonrpc\":\"2.0\",\"method\":\"debug_traceTransaction\",\"params\":[\"$tx\",{\"tracer\":\"callTracer\",\"tracerConfig\":{\"onlyTopCall\":true}}],\"id\":1}")
			if printf '%s' "$trace" | jq -e --argjson full "$full_trace" \
				'.error == null and .result.type == "CALL" and
				(.result | has("calls") | not) and .result == ($full.result | del(.calls))' >/dev/null; then
				ok "onlyTopCall works (the Blockscout path)"
			else
				bad "onlyTopCall works (the Blockscout path)" "$trace"
			fi
		fi
	fi

	trace=$(rpc "$ep" "$(jq -nc --arg block "$block" \
		'{jsonrpc:"2.0",method:"debug_traceBlockByNumber",params:[$block,{tracer:"callTracer"}],id:1}')")
	if printf '%s' "$trace" | jq -e --argjson full "$full_trace" \
		'.error == null and (.result | length == 1) and .result[0].result == $full.result' >/dev/null; then
		ok "debug_traceBlockByNumber returns the mined transaction trace"
	else
		bad "debug_traceBlockByNumber returns the mined transaction trace" "$trace"
	fi

	# -- the destructive surface --------------------------------------------
	# Safe here and nowhere else: this chain was created seconds ago.
	local head_before head_after
	head_before=$(rpc "$ep" '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r .result)

	local m
	for m in debug_setHead debug_startCPUProfile debug_chaindbCompact debug_setGCPercent \
		debug_traceCall debug_traceBlockByHash debug_traceBlockFromFile \
		debug_stacks debug_gcStats debug_writeMemProfile; do
		expect_code "$m rejected over HTTP" "$ep" \
			"{\"jsonrpc\":\"2.0\",\"method\":\"$m\",\"params\":[],\"id\":1}" \
			-32601 "the method $m does not exist/is not available"
	done

	# debug_setHead with the argument it would actually be attacked with.
	expect_code "debug_setHead(0) rejected" "$ep" \
		'{"jsonrpc":"2.0","method":"debug_setHead","params":["0x0"],"id":1}' -32601
	head_after=$(rpc "$ep" '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r .result)
	if [[ "$head_before" =~ ^0x[0-9a-fA-F]+$ ]] && [ "$head_before" = "$head_after" ]; then
		ok "the chain head survived debug_setHead (still $head_after)"
	else
		bad "the chain was rewound" "$head_before -> $head_after"
	fi

	# debug_startCPUProfile with a path, the arbitrary-file-write primitive.
	expect_code "debug_startCPUProfile(path) rejected" "$ep" \
		'{"jsonrpc":"2.0","method":"debug_startCPUProfile","params":["/tmp/pwned.prof"],"id":1}' -32601

	# -- parameter validation ------------------------------------------------
	local t='{"jsonrpc":"2.0","method":"debug_traceBlockByNumber","params":["latest",%s],"id":1}'
	expect_code "no tracer config rejected" "$ep" \
		'{"jsonrpc":"2.0","method":"debug_traceBlockByNumber","params":["latest"],"id":1}' \
		-32602 'tracer is required on this transport and must be "callTracer"'
	expect_code "another native tracer rejected" "$ep" \
		"$(printf "$t" '{"tracer":"prestateTracer"}')" \
		-32602 'tracer must be "callTracer" on this transport'
	expect_code "javascript tracer rejected" "$ep" \
		"$(printf "$t" '{"tracer":"{step:function(){},result:function(){},fault:function(){}}"}')" \
		-32602 'tracer must be "callTracer" on this transport'
	expect_code "struct logger options rejected" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","enableMemory":true}')" \
		-32602 'struct logger options are not available on this transport'
	expect_code "timeout above the cap rejected" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","timeout":"60s"}')" \
		-32602 'timeout 1m0s exceeds the limit of 10s on this transport'
	expect_code "negative timeout rejected" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","timeout":"-1s"}')" \
		-32602 'timeout -1s must be positive'
	expect_code "unparsable timeout rejected" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","timeout":"forever"}')" \
		-32602 'invalid timeout: time: invalid duration "forever"'
	expect_code "reexec above the cap rejected" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","reexec":99999}')" \
		-32602 'reexec 99999 exceeds the limit of 128 on this transport'
	expect_code "unknown tracerConfig field rejected" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","tracerConfig":{"withLog":true}}')" \
		-32602 'invalid tracerConfig: json: unknown field "withLog"'
	expect_code "null tracerConfig rejected" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","tracerConfig":null}')" \
		-32602 'invalid tracerConfig: must be a JSON object'
	expect_code "array tracerConfig rejected" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","tracerConfig":[]}')" \
		-32602 'invalid tracerConfig: must be a JSON object'
	expect_code "null onlyTopCall rejected" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","tracerConfig":{"onlyTopCall":null}}')" \
		-32602 'invalid tracerConfig: onlyTopCall must be a boolean'
	expect_ok "timeout at the cap accepted" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","timeout":"10s"}')"
	expect_ok "reexec at the cap accepted" "$ep" \
		"$(printf "$t" '{"tracer":"callTracer","reexec":128}')"

	# -- batch limits --------------------------------------------------------
	local trace_elem='{"jsonrpc":"2.0","method":"debug_traceBlockByNumber","params":["latest",{"tracer":"callTracer"}],"id":%d}'
	local plain_elem='{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":%d}'
	local resp

	resp=$(rpc "$ep" "$(build_batch 11 "$trace_elem")")
	if [ "$(printf '%s' "$resp" | jq '[.[] | select(.error.code == -32600 and .error.message == "too many trace calls in batch: 11 exceed the limit of 10")] | length')" = 11 ]; then
		ok "11 traces rejected, every element answered"
	else
		bad "11 traces rejected" "$resp"
	fi
	resp=$(rpc "$ep" "$(build_batch 10 "$trace_elem")")
	if [ "$(printf '%s' "$resp" | jq '[.[] | select(has("result"))] | length')" = 10 ]; then
		ok "10 traces accepted (at the limit)"
	else
		bad "10 traces accepted" "$resp"
	fi
	resp=$(rpc "$ep" "$(build_batch 101 "$plain_elem")")
	if [ "$(printf '%s' "$resp" | jq '[.[] | select(.error.code == -32600 and .error.message == "batch too large: 101 requests exceed the limit of 100")] | length')" = 101 ]; then
		ok "101 requests rejected, every element answered"
	else
		bad "101 requests rejected" "$resp"
	fi

	# A batch made only of notifications gets no response at all, so a client
	# is never left waiting on a reply that will not come.
	if resp=$(rpc "$ep" "$(build_batch 11 '{"jsonrpc":"2.0","method":"debug_traceBlockByNumber","params":["latest",{"tracer":"callTracer"}]}')") &&
		[ -z "$(printf '%s' "$resp" | tr -d '[:space:]')" ]; then
		ok "a rejected notification-only batch draws no response"
	else
		bad "a rejected notification-only batch draws no response" "$resp"
	fi

	# -- the WS transport ----------------------------------------------------
	if [ -z "$ws" ]; then
		skipped "WS transport" "no WS endpoint"
	else
		# The console checks arity before it ever reaches the server, so each
		# probe has to be a call the client considers well-formed.
		local got probe m
		for probe in 'debug_setHead|debug.setHead("0x0")' \
			'debug_traceCall|debug.traceCall({},"latest")' \
			'debug_chaindbCompact|debug.chaindbCompact()' \
			'debug_startCPUProfile|debug.startCPUProfile("/tmp/pwned.prof")'; do
			m="${probe%%|*}"
			got=$(attach "$ws" "try{${probe#*|};\"NO ERROR\"}catch(e){e.message}")
			if printf '%s' "$got" | grep -qF "the method $m does not exist/is not available"; then
				ok "$m rejected over WS"
			else
				bad "$m rejected over WS" "$got"
			fi
		done
		got=$(attach "$ws" "var traces=debug.traceBlockByNumber(\"$block\",{tracer:\"callTracer\"}); traces.length===1 && traces[0].result.from===\"$acc\" && traces[0].result.calls.length>0")
		if [ "$got" = true ]; then
			ok "debug_traceBlockByNumber works over WS"
		else
			bad "debug_traceBlockByNumber works over WS" "$got"
		fi
		got=$(attach "$ws" "var trace=debug.traceTransaction(\"$tx\",{tracer:\"callTracer\",tracerConfig:{onlyTopCall:true}}); trace.type===\"CALL\" && trace.from===\"$acc\" && !trace.calls")
		if [ "$got" = true ]; then
			ok "debug_traceTransaction with onlyTopCall works over WS"
		else
			bad "debug_traceTransaction with onlyTopCall works over WS" "$got"
		fi
	fi

	# -- IPC stays unrestricted ---------------------------------------------
	# IPC is an operator-only, filesystem-permissioned socket. The profile must
	# not reach it, or restricting a public transport would also break the
	# operator's own tooling.
	got=$(attach "$NODE_IPC" 'debug.gcStats().NumGC >= 0')
	if [ "$got" = "true" ]; then
		ok "debug_gcStats works over IPC while HTTP rejects it"
	else
		bad "debug_gcStats works over IPC" "$got"
	fi
	got=$(attach "$NODE_IPC" 'typeof debug.traceCall')
	if printf '%s' "$got" | grep -q function; then
		ok "debug_traceCall is present over IPC"
	else
		bad "debug_traceCall is present over IPC" "$got"
	fi
	resp=$(attach "$NODE_IPC" 'debug.setGCPercent(100) !== undefined')
	if [ "$resp" = "true" ]; then
		ok "debug_setGCPercent works over IPC"
	else
		bad "debug_setGCPercent works over IPC" "$resp"
	fi
}

# ---------------------------------------------------------------------------
# Phase 3 — concurrency limit
# ---------------------------------------------------------------------------

phase_concurrency() {
	section "Phase 3: trace concurrency limit"

	local port=$((BASE_PORT + 2))
	if ! start_geth concurrency "$port" 0 \
		--dev --dev.gaslimit 30000000 --miner.gaslimit 30000000 \
		--port 0 --authrpc.port 0 --nodiscover --maxpeers 0 \
		--http --http.api eth,net,web3,debug \
		--http.debug-profile trace-indexer-v1 \
		--http.debug-trace.max-concurrency 1; then
		bad "concurrency node did not start" "$(node_log | tail -3)"
		return
	fi

	local ep="$NODE_HTTP" acc tx receipt contract payload i
	acc=$(rpc "$ep" '{"jsonrpc":"2.0","method":"eth_accounts","params":[],"id":1}' | jq -r '.result[0] // empty')
	# Deploy JUMPDEST; PUSH1 0; JUMP, then mine a call that exhausts 16M gas.
	# Replaying it holds a slot long enough for overlapping HTTP requests even
	# on a fast dev node. A plain transfer finishes before contention develops.
	tx=$(rpc "$ep" "$(jq -nc --arg from "$acc" \
		'{jsonrpc:"2.0",method:"eth_sendTransaction",params:[{from:$from,gas:"0x20000",
		data:"0x6004600c60003960046000f35b600056"}],id:1}')" | jq -r '.result // empty')
	receipt=$(wait_receipt "$ep" "$tx")
	contract=$(printf '%s' "$receipt" | jq -r '.result.contractAddress // empty')
	if [ -z "$contract" ] || [ "$(printf '%s' "$receipt" | jq -r '.result.status')" != "0x1" ]; then
		bad "concurrency fixture deployed"
		return
	fi
	tx=$(rpc "$ep" "$(jq -nc --arg from "$acc" --arg to "$contract" \
		'{jsonrpc:"2.0",method:"eth_sendTransaction",params:[{from:$from,to:$to,gas:"0xf42400"}],id:1}')" | jq -r '.result // empty')
	receipt=$(wait_receipt "$ep" "$tx")
	if ! printf '%s' "$receipt" | jq -e '.result.status == "0x0" and .result.gasUsed == "0xf42400"' >/dev/null; then
		bad "concurrency fixture consumed its gas budget"
		return
	fi
	payload=$(jq -nc --arg tx "$tx" \
		'{jsonrpc:"2.0",method:"debug_traceTransaction",params:[$tx,{tracer:"callTracer",timeout:"10s"}],id:1}')

	# Wait on these PIDs specifically: a bare `wait` would also wait for the
	# node. Separate files prevent concurrent response writes from interleaving.
	local probes=()
	for i in $(seq 1 8); do
		rpc "$ep" "$payload" >"$WORKDIR/concurrency.$i.json" &
		probes+=($!)
	done
	local transport_failed=0
	for i in "${probes[@]}"; do
		wait "$i" || transport_failed=1
	done
	if [ "$transport_failed" -eq 0 ] && jq -se '
		def traced: .error == null and .result.type == "CALL" and
			.result.error == "out of gas";
		def saturated: .error.code == -32005 and .error.message == "too many concurrent traces";
		length == 8 and all(.[]; .jsonrpc == "2.0" and .id == 1 and (traced or saturated)) and
		any(.[]; traced) and any(.[]; saturated)
	' "$WORKDIR"/concurrency.*.json >/dev/null; then
		ok "saturation returns -32005 too many concurrent traces"
	else
		bad "saturation returns -32005 too many concurrent traces" \
			"expected both completed traces and limiter rejections; acceptance cannot skip contention"
	fi
	expect_code "the concurrency slot is released after tracing" "$ep" \
		'{"jsonrpc":"2.0","method":"debug_traceTransaction","params":["0x0000000000000000000000000000000000000000000000000000000000000000",{"tracer":"callTracer"}],"id":1}' \
		-32000 'transaction not found'
}

# ---------------------------------------------------------------------------
# Phase 4 — admin_startWS re-validation
#
# admin is exposed on some deployments. Without the re-check, anyone reaching
# it could reopen a transport carrying the full debug namespace, bypassing the
# startup validation entirely.
# ---------------------------------------------------------------------------

phase_admin() {
	section "Phase 4: admin_startWS re-validation"

	# The WS port is published even though WS is not enabled at startup: the
	# whole point is to ask the running node to open it, then prove from the
	# outside that it did not. Probing a port the node could never have exposed
	# to the host would pass no matter what the node did.
	local port=$((BASE_PORT + 3)) ws_port=$((BASE_PORT + 4))
	if ! start_geth admin "$port" "$ws_port" \
		--dev --port 0 --authrpc.port 0 --nodiscover --maxpeers 0 \
		--http --http.api eth,net,web3,admin,debug \
		--http.debug-profile trace-indexer-v1; then
		bad "admin node did not start" "$(node_log | tail -3)"
		return
	fi

	local ep="$NODE_HTTP" body msg
	body=$(rpc "$ep" "{\"jsonrpc\":\"2.0\",\"method\":\"admin_startWS\",\"params\":[\"$NODE_BIND_ADDR\",$NODE_WS_INNER,\"*\",\"debug\"],\"id\":1}")
	msg=$(printf '%s' "$body" | jq -r '.error.message // empty')
	if printf '%s' "$msg" | grep -qF 'the "debug" namespace is exposed on --ws.api without --ws.debug-profile'; then
		ok "admin_startWS refuses to open an unrestricted debug transport"
	else
		bad "admin_startWS refuses to open an unrestricted debug transport" "$body"
	fi

	# And the transport really was not opened. This asks the port for a
	# JSON-RPC answer rather than just completing a TCP handshake: under docker
	# the published host port is held open by docker-proxy whether or not
	# anything listens inside the container, so a bare connect would report
	# every refused transport as open. attach dials from the node's own side,
	# where only a real server can answer.
	local probe
	probe=$(attach "ws://127.0.0.1:$ws_port" '1+1')
	if [ "$probe" = 2 ]; then
		bad "no listener was opened on the refused port" "a WS server answered on $ws_port"
	else
		ok "no listener was opened on the refused port"
	fi
}

# ---------------------------------------------------------------------------
# Phase 5 — a non-default batch limit
#
# Every other batch check runs at the default of 100, which never executes the
# flag-to-config plumbing behind --http.rpc.batch-limit: a flag that silently
# failed to take effect would look identical.
# ---------------------------------------------------------------------------

phase_batch_limit_flag() {
	section "Phase 5: non-default --http.rpc.batch-limit"

	local port=$((BASE_PORT + 5))
	if ! start_geth batchlimit "$port" 0 \
		--dev --port 0 --authrpc.port 0 --nodiscover --maxpeers 0 \
		--http --http.api eth,net,web3,debug \
		--http.debug-profile trace-indexer-v1 \
		--http.rpc.batch-limit 5; then
		bad "batch-limit node did not start" "$(node_log | tail -3)"
		return
	fi

	local ep="$NODE_HTTP" resp
	local plain_elem='{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":%d}'

	resp=$(rpc "$ep" "$(build_batch 6 "$plain_elem")")
	if [ "$(printf '%s' "$resp" | jq '[.[] | select(.error.code == -32600 and .error.message == "batch too large: 6 requests exceed the limit of 5")] | length')" = 6 ]; then
		ok "6 requests rejected against a limit of 5"
	else
		bad "6 requests rejected against a limit of 5" "$resp"
	fi

	resp=$(rpc "$ep" "$(build_batch 5 "$plain_elem")")
	if [ "$(printf '%s' "$resp" | jq '[.[] | select(has("result"))] | length')" = 5 ]; then
		ok "5 requests accepted (at the configured limit)"
	else
		bad "5 requests accepted (at the configured limit)" "$resp"
	fi

	# The default must no longer apply, or the flag did nothing.
	resp=$(rpc "$ep" "$(build_batch 100 "$plain_elem")")
	if [ "$(printf '%s' "$resp" | jq '[.[] | select(.error.code == -32600)] | length')" = 100 ]; then
		ok "the default limit of 100 no longer applies"
	else
		bad "the default limit of 100 no longer applies" "$resp"
	fi

	# --ws.rpc.batch-limit has its own branch in cmd/utils/flags.go and is not
	# reachable from here: a genuine JSON-RPC batch over a WebSocket needs a
	# real WS client, and geth's console splits a batch into separate calls, so
	# it returns results for a batch the server would have rejected. Covered by
	# TestWSBatchLimitFlag in cmd/geth instead.
	skipped "the WS batch limit is honoured" \
		"needs a WS client; covered by cmd/geth TestWSBatchLimitFlag"
}

# ---------------------------------------------------------------------------
# Phase 6 — the unsafe opt-in
#
# The matrix row is "full namespace, one WARN". The warning is asserted in
# phase 1; this asserts the other half over the wire, so that an opt-in which
# warned but quietly restricted anyway could not pass.
# ---------------------------------------------------------------------------

phase_unsafe() {
	section "Phase 6: the unsafe opt-in serves the full namespace"

	local port=$((BASE_PORT + 6))
	if ! start_geth unsafe "$port" 0 \
		--dev --port 0 --authrpc.port 0 --nodiscover --maxpeers 0 \
		--http --http.api eth,net,web3,debug \
		--http.allow-unsafe-debug; then
		bad "unsafe node did not start" "$(node_log | tail -3)"
		return
	fi

	local ep="$NODE_HTTP"
	# Read-only members of the namespace the profile withholds. Destructive ones
	# are not called: the point is that they are registered, and phase 2 already
	# proves a profiled transport withholds them.
	expect_ok "debug_gcStats is served" "$ep" \
		'{"jsonrpc":"2.0","method":"debug_gcStats","params":[],"id":1}'
	expect_ok "debug_memStats is served" "$ep" \
		'{"jsonrpc":"2.0","method":"debug_memStats","params":[],"id":1}'

	# Argument validation proves registration without depending on whether the
	# legacy traceCall implementation can execute on a fresh genesis state.
	expect_code "debug_traceCall is registered" "$ep" \
		'{"jsonrpc":"2.0","method":"debug_traceCall","params":[],"id":1}' \
		-32602 'missing value for required argument 0'

	# And the profile's parameter validation does not apply here: a tracer the
	# profile rejects with -32602 must get through. Whether the block itself can
	# be traced is a property of the chain, so only the rejection matters.
	local body
	body=$(rpc "$ep" '{"jsonrpc":"2.0","method":"debug_traceBlockByNumber","params":["latest",{"tracer":"prestateTracer"}],"id":1}')
	if printf '%s' "$body" | jq -e '
		(.error == null and (.result | type == "array")) or
		(.error.code == -32000 and .error.message == "genesis is not traceable")' >/dev/null 2>&1; then
		ok "an unrestricted tracer config is accepted"
	else
		bad "an unrestricted tracer config is accepted" "$body"
	fi
}

# ---------------------------------------------------------------------------

echo "=== debug profile verification ==="
prepare_runner

phase_matrix
phase_restricted
phase_concurrency
phase_admin
phase_batch_limit_flag
phase_unsafe

printf '\n=== %d passed, %d failed, %d skipped ===\n' "$pass" "$fail" "$skip"
[ "$fail" -eq 0 ] || exit 1
