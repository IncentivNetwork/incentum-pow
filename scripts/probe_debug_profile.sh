#!/bin/bash
#
# probe_debug_profile.sh — verify the fail-closed debug RPC profile on a LIVE
# node using read-only requests.
#
# The restricted profile is what stands between a public RPC endpoint and
# debug_setHead, arbitrary-path profile files and chain-db compaction. This
# script confirms the restriction is in force, but it deliberately never calls
# a destructive or state-changing method: if the protection were broken, the
# probe itself would be the outage. It starts with two harmless methods that a
# profiled transport must reject, and refuses to go any further unless both
# come back -32601.
#
# Load: nothing here changes node state, but tracing is not free. The probe
# issues a handful of single-transaction traces and two at-limit batches of ten
# trace calls. Traces of blocks are only requested where a cheaper call cannot
# make the point. On a busy archive node prefer a quiet moment.
#
# For the destructive half of the matrix — and for the configuration matrix,
# which needs restarts — use scripts/test_debug_profile.sh against a local
# container instead.
#
# Usage:
#   ./scripts/probe_debug_profile.sh http://host:8545
#
# Environment:
#   PROBE_TIMEOUT      per-request timeout in seconds (default 20)
#   PROBE_UA           User-Agent to send; unset means curl's own
#   PROBE_SCAN_BLOCKS  how far back to look for a transaction to trace
#                      (default 50)
#
# Requirements: curl, jq
# Output deliberately omits endpoint, node metadata and raw RPC responses so
# both successful and failed runs can be shared without disclosing the node.

set -uo pipefail

ENDPOINT="${1:-}"
if [ -z "$ENDPOINT" ]; then
	echo "usage: $0 <http-rpc-endpoint>" >&2
	exit 2
fi

TIMEOUT="${PROBE_TIMEOUT:-20}"
PROBE_UA="${PROBE_UA:-}"

pass=0
fail=0
skip=0

ok() {
	printf '  \033[32mPASS\033[0m %s\n' "$1"
	pass=$((pass + 1))
}

bad() {
	printf '  \033[31mFAIL\033[0m %s\n' "$1"
	# Details may contain server responses, addresses or credentials. Keep
	# them out of shareable evidence, including on failure paths.
	fail=$((fail + 1))
}

# skipped reports a check that could not be exercised. It is never counted as a
# pass: a check that did not run proves nothing.
skipped() {
	printf '  \033[33mSKIP\033[0m %s\n' "$1"
	if [ $# -gt 1 ]; then
		printf '       %s\n' "$2"
	fi
	skip=$((skip + 1))
}

# rpc_call POSTs a JSON-RPC payload and prints the raw response body.
rpc_call() {
	local ua=()
	[ -n "$PROBE_UA" ] && ua=(-A "$PROBE_UA")
	# Disable curlrc (which may enable verbose output) and suppress diagnostics
	# containing the URL or response. Preserve failure as a non-zero status.
	curl -q -sf --proto '=http,https' --max-time "$TIMEOUT" \
		"${ua[@]}" \
		-H 'Content-Type: application/json' \
		--data "$1" \
		--url "$ENDPOINT" 2>/dev/null
}

# jq diagnostics can quote response data too. Callers check status or output.
jq() { command jq "$@" 2>/dev/null; }

# expect_error asserts that a call fails with a given code, and optionally with
# an exact message.
expect_error() {
	local label="$1" payload="$2" want_code="$3" want_msg="${4:-}"
	local body code msg

	body=$(rpc_call "$payload")
	if [ -z "$body" ]; then
		bad "$label" "empty response"
		return
	fi
	code=$(printf '%s' "$body" | jq -r '.error.code // empty' 2>/dev/null)
	msg=$(printf '%s' "$body" | jq -r '.error.message // empty' 2>/dev/null)
	if [ -z "$code" ]; then
		bad "$label" "expected error $want_code, got: $body"
		return
	fi
	if [ "$code" != "$want_code" ]; then
		bad "$label" "code $code, want $want_code ($msg)"
		return
	fi
	if [ -n "$want_msg" ] && [ "$msg" != "$want_msg" ]; then
		bad "$label" "message: $msg
       want:    $want_msg"
		return
	fi
	ok "$label"
}

trace_params() { # $1 = extra config fields
	printf '{"jsonrpc":"2.0","method":"debug_traceBlockByNumber","params":["latest",%s],"id":1}' "$1"
}

trace_tx() { # $1 = tx hash, $2 = extra config fields
	printf '{"jsonrpc":"2.0","method":"debug_traceTransaction","params":["%s",%s],"id":1}' "$1" "$2"
}

# ZERO_HASH names no transaction. Calls using it reach the tracer, pass
# validation and fail cheaply, which is all that is needed to prove a method is
# registered or that a batch was not rejected wholesale.
ZERO_HASH=0x0000000000000000000000000000000000000000000000000000000000000000

# build_batch <count> <method-json-template>
build_batch() {
	local count="$1" template="$2" i out=""
	for ((i = 0; i < count; i++)); do
		[ -n "$out" ] && out="$out,"
		out="$out$(printf "$template" "$i")"
	done
	printf '[%s]' "$out"
}

# The trace element names no real transaction on purpose. The per-batch trace
# cap is counted from the method name before any call runs, so a batch of these
# proves the cap without asking the node to trace anything.
trace_elem='{"jsonrpc":"2.0","method":"debug_traceTransaction","params":["'"$ZERO_HASH"'",{"tracer":"callTracer"}],"id":%d}'
plain_elem='{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":%d}'

# TRACE_CAP is fixed in rpc/debugprofile.go and not operator-configurable.
TRACE_CAP=10

# BATCH_LIMIT is whatever this node enforces, which #119 makes configurable.
# 0 means "not determined": either no cap, or one above the probe size.
BATCH_LIMIT=0

# detect_batch_limit asks the node for its own limit rather than assuming the
# default. An over-sized batch names the limit in its error message, so one
# cheap request settles it; every later expectation is derived from the answer.
detect_batch_limit() {
	local resp msg
	resp=$(rpc_call "$(build_batch 101 "$plain_elem")")
	msg=$(printf '%s' "$resp" |
		jq -r 'if type == "array" then ([.[] | .error.message] | map(select(. != null)) | first) else .error.message end // empty' 2>/dev/null)
	case "$msg" in
	*"exceed the limit of "*) BATCH_LIMIT="${msg##*exceed the limit of }" ;;
	esac
	case "$BATCH_LIMIT" in
	'' | *[!0-9]*) BATCH_LIMIT=0 ;;
	esac
}

# expect_reachable asserts a method is registered and accepted the arguments.
# It deliberately tolerates -32000: whether a particular block or transaction
# can be traced is a property of the chain, not of the profile. A node whose
# head is the genesis block answers "genesis is not traceable" to every trace,
# and that says nothing about whether the restriction works.
expect_reachable() {
	local label="$1" payload="$2"
	local body code msg

	body=$(rpc_call "$payload")
	code=$(printf '%s' "$body" | jq -r '.error.code // empty' 2>/dev/null)
	msg=$(printf '%s' "$body" | jq -r '.error.message // empty' 2>/dev/null)
	case "$code" in
	"") ok "$label" ;;
	-32601) bad "$label" "method is not registered: $msg" ;;
	-32602) bad "$label" "arguments were rejected: $msg" ;;
	*) ok "$label (reachable)" ;;
	esac
}

# find_traceable_tx looks for a recent transaction to trace, batching the block
# lookups so a remote endpoint is not hit with a round trip per block. The
# chunk size follows the node's own batch cap: one oversized request would be
# rejected wholesale and the probe would then report "no transactions" for a
# chain that has plenty. Leaves TRACE_TX empty only when the scanned range
# really holds none.
TRACE_TX=""
find_traceable_tx() {
	local head scan chunk scanned n i batch out
	head=$(rpc_call '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | jq -r '.result // empty')
	[ -z "$head" ] && return
	head=$((head))
	scan="${PROBE_SCAN_BLOCKS:-50}"
	[ "$scan" -gt "$head" ] && scan="$head"
	[ "$scan" -le 0 ] && return

	chunk=50
	[ "$BATCH_LIMIT" -gt 0 ] && [ "$BATCH_LIMIT" -lt "$chunk" ] && chunk="$BATCH_LIMIT"

	scanned=0
	while [ "$scanned" -lt "$scan" ]; do
		n=$((scan - scanned))
		[ "$n" -gt "$chunk" ] && n="$chunk"

		batch=""
		for ((i = 0; i < n; i++)); do
			[ -n "$batch" ] && batch="$batch,"
			batch="$batch$(printf '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x%x",false],"id":%d}' $((head - scanned - i)) "$i")"
		done
		out=$(rpc_call "[$batch]")
		# Ids ascend as the scan walks backwards, so the smallest id holding
		# transactions is the newest such block.
		TRACE_TX=$(printf '%s' "$out" |
			jq -r '[.[]? | select((.result.transactions // []) | length > 0)] | min_by(.id) | .result.transactions[0] // empty' 2>/dev/null)
		[ "$TRACE_TX" = null ] && TRACE_TX=""
		[ -n "$TRACE_TX" ] && return

		scanned=$((scanned + n))
	done
}

echo "=== Probing debug RPC profile ==="
echo

# ---------------------------------------------------------------------------
# Reachability
# ---------------------------------------------------------------------------
echo "--- Node ---"
chain_id=$(rpc_call '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' | jq -r '.result // empty')
if [ -z "$chain_id" ]; then
	echo "  node did not answer eth_chainId" >&2
	exit 2
fi
echo "  node answered eth_chainId (metadata omitted)"
echo

# ---------------------------------------------------------------------------
# Safety gate. Two methods that are harmless to call but must be absent on a
# profiled transport. If either answers, the profile is NOT in force and this
# script stops: probing further would mean calling destructive methods against
# a node that has no protection.
# ---------------------------------------------------------------------------
echo "--- Safety gate ---"
gate_open=0
for method in debug_gcStats debug_traceCall; do
	case "$method" in
	debug_traceCall) payload='{"jsonrpc":"2.0","method":"debug_traceCall","params":[{},"latest"],"id":1}' ;;
	*) payload="{\"jsonrpc\":\"2.0\",\"method\":\"$method\",\"params\":[],\"id\":1}" ;;
	esac
	code=$(rpc_call "$payload" | jq -r '.error.code // empty')
	if [ "$code" != "-32601" ]; then
		gate_open=1
		printf '  \033[31m%s did not return -32601\033[0m\n' "$method"
	else
		ok "$method is not registered"
	fi
done
if [ "$gate_open" -ne 0 ]; then
	cat >&2 <<-'EOF'

		  ####################################################################
		  #  The debug namespace is NOT restricted on this endpoint.         #
		  #  Stopping here: the remaining checks would call methods that     #
		  #  can rewind the chain or write files on the node.                #
		  #                                                                  #
		  #  Start the node with --http.debug-profile trace-indexer-v1       #
		  #  (see docs/dpow/DPOW_NODE_OPERATOR_GUIDE.md §3.1).               #
		  ####################################################################
	EOF
	exit 1
fi
echo

# ---------------------------------------------------------------------------
# Namespace visibility
# ---------------------------------------------------------------------------
echo "--- Namespace ---"
modules=$(rpc_call '{"jsonrpc":"2.0","method":"rpc_modules","params":[],"id":1}' | jq -r '.result // empty')
if printf '%s' "$modules" | jq -e 'has("debug")' >/dev/null 2>&1; then
	ok "rpc_modules still advertises debug (namespaces, not per-method capabilities)"
else
	bad "rpc_modules does not list debug" "$modules"
fi
if printf '%s' "$modules" | jq -e 'has("admin")' >/dev/null 2>&1; then
	printf '  \033[33mWARN\033[0m admin namespace is exposed on this endpoint.\n'
	printf '       admin_startHTTP/admin_startWS can reopen a transport; the profile\n'
	printf '       re-check is the only thing preventing a full debug surface.\n'
fi
echo

# ---------------------------------------------------------------------------
# Method surface. Only non-destructive methods are probed here: the destructive
# ones are covered by the local container matrix.
# ---------------------------------------------------------------------------
echo "--- Rejected methods ---"
for method in debug_traceCall debug_traceBlockByHash debug_traceBlockFromFile \
	debug_stacks debug_gcStats debug_memStats debug_getRawHeader \
	debug_intermediateRoots debug_standardTraceBlockToFile debug_traceChain; do
	expect_error "$method -> -32601" \
		"{\"jsonrpc\":\"2.0\",\"method\":\"$method\",\"params\":[],\"id\":1}" \
		-32601 \
		"the method $method does not exist/is not available"
done
echo

# The batch caps are operator-configurable, and the block scan below batches
# too, so establish what this node actually enforces before either is used.
detect_batch_limit
if [ "$BATCH_LIMIT" -gt 0 ]; then
	echo "--- Limits ---"
	echo "  batch elements: $BATCH_LIMIT"
	echo
fi

echo "--- Allowed methods ---"
expect_reachable "debug_traceBlockByNumber is registered" \
	"$(trace_params '{"tracer":"callTracer"}')"
expect_reachable "debug_traceTransaction is registered" \
	"$(trace_tx "$ZERO_HASH" '{"tracer":"callTracer"}')"
expect_reachable "onlyTopCall is accepted (the Blockscout path)" \
	"$(trace_tx "$ZERO_HASH" '{"tracer":"callTracer","tracerConfig":{"onlyTopCall":true}}')"

# Registration is not function. Trace a real transaction when the chain offers
# one; an idle or freshly started node has nothing to trace, and that is
# reported rather than passed.
find_traceable_tx
if [ -z "$TRACE_TX" ]; then
	skipped "debug_traceTransaction returns a real call trace" \
		"no transaction in the last ${PROBE_SCAN_BLOCKS:-50} blocks"
	skipped "onlyTopCall returns a real call trace" \
		"no transaction in the last ${PROBE_SCAN_BLOCKS:-50} blocks"
else
	body=$(rpc_call "$(trace_tx "$TRACE_TX" '{"tracer":"callTracer"}')")
	if [ -n "$(printf '%s' "$body" | jq -r '.result.type // empty')" ]; then
		ok "debug_traceTransaction returns a real call trace"
	else
		bad "debug_traceTransaction returns a real call trace" "$body"
	fi
	body=$(rpc_call "$(trace_tx "$TRACE_TX" '{"tracer":"callTracer","tracerConfig":{"onlyTopCall":true}}')")
	if [ -n "$(printf '%s' "$body" | jq -r '.result.type // empty')" ]; then
		ok "onlyTopCall returns a real call trace"
	else
		bad "onlyTopCall returns a real call trace" "$body"
	fi
fi
echo

# ---------------------------------------------------------------------------
# Parameter validation on the allowed methods.
# ---------------------------------------------------------------------------
echo "--- Parameter validation ---"
expect_error "no tracer config" \
	'{"jsonrpc":"2.0","method":"debug_traceBlockByNumber","params":["latest"],"id":1}' \
	-32602 'tracer is required on this transport and must be "callTracer"'
expect_error "another native tracer" \
	"$(trace_params '{"tracer":"prestateTracer"}')" \
	-32602 'tracer must be "callTracer" on this transport'
expect_error "javascript tracer" \
	"$(trace_params '{"tracer":"{step:function(){},result:function(){},fault:function(){}}"}')" \
	-32602 'tracer must be "callTracer" on this transport'
expect_error "struct logger options" \
	"$(trace_params '{"tracer":"callTracer","enableMemory":true}')" \
	-32602 'struct logger options are not available on this transport'
expect_error "timeout above the cap" \
	"$(trace_params '{"tracer":"callTracer","timeout":"60s"}')" \
	-32602 'timeout 1m0s exceeds the limit of 10s on this transport'
expect_error "unparsable timeout" \
	"$(trace_params '{"tracer":"callTracer","timeout":"forever"}')" \
	-32602 'invalid timeout: time: invalid duration "forever"'
expect_error "reexec above the cap" \
	"$(trace_params '{"tracer":"callTracer","reexec":99999}')" \
	-32602 'reexec 99999 exceeds the limit of 128 on this transport'
expect_error "unknown tracerConfig field" \
	"$(trace_params '{"tracer":"callTracer","tracerConfig":{"withLog":true}}')" \
	-32602 'invalid tracerConfig: json: unknown field "withLog"'
expect_error "null tracerConfig" \
	"$(trace_params '{"tracer":"callTracer","tracerConfig":null}')" \
	-32602 'invalid tracerConfig: must be a JSON object'
expect_error "array tracerConfig" \
	"$(trace_params '{"tracer":"callTracer","tracerConfig":[]}')" \
	-32602 'invalid tracerConfig: must be a JSON object'
expect_error "null onlyTopCall" \
	"$(trace_params '{"tracer":"callTracer","tracerConfig":{"onlyTopCall":null}}')" \
	-32602 'invalid tracerConfig: onlyTopCall must be a boolean'
echo

# ---------------------------------------------------------------------------
# Batch limits. Every element of a rejected batch is answered individually so a
# client waiting on the batch resolves instead of hanging.
# ---------------------------------------------------------------------------
echo "--- Batch limits ---"

check_batch() {
	local label="$1" body="$2" want_code="$3" want_msg="$4"
	local resp count bad_elems

	resp=$(rpc_call "$body")
	count=$(printf '%s' "$resp" | jq 'length' 2>/dev/null)
	if [ -z "$count" ] || [ "$count" = "null" ]; then
		bad "$label" "not a batch response: $resp"
		return
	fi
	bad_elems=$(printf '%s' "$resp" |
		jq --argjson c "$want_code" --arg m "$want_msg" \
			'[.[] | select(.error.code != $c or .error.message != $m)] | length')
	if [ "$bad_elems" != "0" ]; then
		bad "$label" "$bad_elems of $count elements carried a different error"
		return
	fi
	ok "$label ($count elements, each answered individually)"
}

# The element cap. Expectations come from the limit this node reported, so a
# node hardened with a lower --http.rpc.batch-limit is verified against its own
# configuration instead of being failed for not using the default.
if [ "$BATCH_LIMIT" -eq 0 ]; then
	skipped "the element cap is enforced" \
		"no cap fired at 101 elements; it is disabled or set above that"
	skipped "a batch at the element cap is accepted" \
		"the cap could not be determined"
else
	over=$((BATCH_LIMIT + 1))
	check_batch "$over requests rejected" \
		"$(build_batch "$over" "$plain_elem")" \
		-32600 "batch too large: $over requests exceed the limit of $BATCH_LIMIT"

	resp=$(rpc_call "$(build_batch "$BATCH_LIMIT" "$plain_elem")")
	if [ "$(printf '%s' "$resp" | jq '[.[] | select(has("result"))] | length')" = "$BATCH_LIMIT" ]; then
		ok "$BATCH_LIMIT requests accepted (at the element cap)"
	else
		bad "$BATCH_LIMIT requests accepted (at the element cap)" "$resp"
	fi
fi

# The trace cap. It can only be observed when the element cap leaves room for
# more than TRACE_CAP elements; below that the element cap fires first and the
# trace cap is never reached.
if [ "$BATCH_LIMIT" -ne 0 ] && [ "$BATCH_LIMIT" -le "$TRACE_CAP" ]; then
	skipped "the trace cap is enforced" \
		"the element cap of $BATCH_LIMIT fires before $((TRACE_CAP + 1)) trace calls"
	skipped "a batch at the trace cap is accepted" \
		"the element cap of $BATCH_LIMIT fires first"
else
	over=$((TRACE_CAP + 1))
	check_batch "$over traces rejected" \
		"$(build_batch "$over" "$trace_elem")" \
		-32600 "too many trace calls in batch: $over exceed the limit of $TRACE_CAP"

	# At the cap the batch must be let through. What each call then returns is
	# a property of the chain, so the assertion is only that nothing was
	# rejected by a batch cap.
	resp=$(rpc_call "$(build_batch "$TRACE_CAP" "$trace_elem")")
	if [ "$(printf '%s' "$resp" | jq '[.[] | select(.error.code == -32600)] | length')" = "0" ]; then
		ok "$TRACE_CAP traces accepted (at the trace cap)"
	else
		bad "$TRACE_CAP traces rejected by a batch cap" "$resp"
	fi
fi
echo

# ---------------------------------------------------------------------------
echo "=== $pass passed, $fail failed, $skip skipped ==="
[ "$fail" -eq 0 ] || exit 1
