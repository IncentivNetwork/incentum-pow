#!/bin/bash
#
# probe_monitor_namespace.sh — verify the read-only monitor RPC namespace on a
# LIVE node.
#
# The namespace exists so monitoring can be served without exposing admin. This
# script checks that it is enabled, that it answers with the documented payload,
# and — where admin happens to still be exposed — that it reports the same
# values monitoring used to take from admin. Both monitor methods are read-only
# and cheap, so this is safe to run against a production node at any time; no
# method it calls changes node state.
#
# Usage:
#   ./scripts/probe_monitor_namespace.sh http://host:8545
#
# Environment:
#   PROBE_TIMEOUT  per-request timeout in seconds (default 20)
#   PROBE_UA       User-Agent to send; unset means curl's own
#
# Requirements: curl, jq

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
	[ $# -gt 1 ] && printf '       %s\n' "$2"
	fail=$((fail + 1))
}

# skipped reports a check that could not be exercised. It is never counted as a
# pass: a check that did not run proves nothing.
skipped() {
	printf '  \033[33mSKIP\033[0m %s\n' "$1"
	[ $# -gt 1 ] && printf '       %s\n' "$2"
	skip=$((skip + 1))
}

rpc_call() {
	local ua=()
	[ -n "$PROBE_UA" ] && ua=(-A "$PROBE_UA")
	curl -sS --max-time "$TIMEOUT" \
		"${ua[@]}" \
		-H 'Content-Type: application/json' \
		--data "$1" \
		"$ENDPOINT"
}

call() { # method -> raw response
	rpc_call "{\"jsonrpc\":\"2.0\",\"method\":\"$1\",\"params\":[],\"id\":1}"
}

echo "=== Probing $ENDPOINT ==="
echo

# ---------------------------------------------------------------------------
echo "--- Node ---"
chain_id=$(call eth_chainId | jq -r '.result // empty')
if [ -z "$chain_id" ]; then
	echo "  node did not answer eth_chainId; is $ENDPOINT reachable?" >&2
	exit 2
fi
echo "  chainId: $chain_id"
echo "  client:  $(call web3_clientVersion | jq -r '.result // empty')"
echo

# ---------------------------------------------------------------------------
# Gate. Everything below assumes the namespace is served here. If it is not,
# say so plainly rather than reporting a wall of failures that all mean the
# same thing.
# ---------------------------------------------------------------------------
echo "--- Namespace ---"
modules=$(call rpc_modules | jq -r '.result // empty')
if ! printf '%s' "$modules" | jq -e 'has("monitor")' >/dev/null 2>&1; then
	cat >&2 <<-EOF

		  The monitor namespace is not enabled on this endpoint.

		  Nothing here can be verified against it. Add "monitor" to --http.api
		  (see docs/dpow/DPOW_NODE_OPERATOR_GUIDE.md) and run this again.
	EOF
	exit 2
fi
ok "rpc_modules advertises monitor"

admin_exposed=0
if printf '%s' "$modules" | jq -e 'has("admin")' >/dev/null 2>&1; then
	admin_exposed=1
	printf '  \033[33mWARN\033[0m admin is exposed on this endpoint as well.\n'
	printf '       The monitor namespace exists so monitoring does not need it;\n'
	printf '       nothing below requires admin either.\n'
fi
echo

# ---------------------------------------------------------------------------
echo "--- monitor_nodeInfo ---"
info=$(call monitor_nodeInfo)
if ! printf '%s' "$info" | jq -e 'has("result")' >/dev/null 2>&1; then
	bad "monitor_nodeInfo answers" "$info"
else
	ok "monitor_nodeInfo answers"

	result=$(printf '%s' "$info" | jq -c '.result')
	for field in name ip listenAddr ports network difficulty; do
		if printf '%s' "$result" | jq -e --arg f "$field" 'has($f)' >/dev/null 2>&1; then
			ok "nodeInfo carries $field"
		else
			bad "nodeInfo carries $field" "$result"
		fi
	done
	for field in listener discovery; do
		if printf '%s' "$result" | jq -e --arg f "$field" '.ports | has($f)' >/dev/null 2>&1; then
			ok "nodeInfo carries ports.$field"
		else
			bad "nodeInfo carries ports.$field" "$result"
		fi
	done

	# difficulty is a *big.Int, which marshals as a bare JSON number. A
	# monitoring stack parsing it as one breaks if that ever becomes a string.
	if [ "$(printf '%s' "$result" | jq -r '.difficulty | type')" = "number" ]; then
		ok "difficulty is a JSON number, not a string"
	else
		bad "difficulty is a JSON number, not a string" \
			"type is $(printf '%s' "$result" | jq -r '.difficulty | type')"
	fi

	if [ "$(printf '%s' "$result" | jq -r '.network')" != "0" ]; then
		ok "network is populated from the eth protocol"
	else
		bad "network is populated from the eth protocol" \
			"network is 0; the eth protocol info was not extracted"
	fi
fi
echo

# ---------------------------------------------------------------------------
echo "--- monitor_peerCount ---"
peers=$(call monitor_peerCount)
peer_count=$(printf '%s' "$peers" | jq -r 'if (.result | type) == "number" then .result else empty end')
if [ -z "$peer_count" ]; then
	bad "monitor_peerCount answers with a number" "$peers"
else
	ok "monitor_peerCount answers with a number ($peer_count)"
fi
echo

# ---------------------------------------------------------------------------
# Cross-check. The namespace was introduced to replace two specific readings
# monitoring used to take from admin. Where admin is still reachable, the
# replacement can be checked against the thing it replaced.
# ---------------------------------------------------------------------------
echo "--- Agreement with admin ---"
if [ "$admin_exposed" -eq 0 ]; then
	skipped "monitor_peerCount matches len(admin_peers)" "admin is not exposed here"
	skipped "monitor_nodeInfo agrees with admin_nodeInfo" "admin is not exposed here"
else
	admin_peers=$(call admin_peers | jq -r 'if (.result | type) == "array" then (.result | length) else empty end')
	if [ -z "$admin_peers" ]; then
		skipped "monitor_peerCount matches len(admin_peers)" "admin_peers did not answer with a list"
	elif [ "$admin_peers" = "$peer_count" ]; then
		ok "monitor_peerCount matches len(admin_peers) ($peer_count)"
	else
		bad "monitor_peerCount matches len(admin_peers)" \
			"monitor says $peer_count, admin says $admin_peers"
	fi

	admin_info=$(call admin_nodeInfo | jq -c '.result // empty')
	if [ -z "$admin_info" ]; then
		skipped "monitor_nodeInfo agrees with admin_nodeInfo" "admin_nodeInfo did not answer"
	else
		mismatch=""
		for field in name ip listenAddr; do
			a=$(printf '%s' "$admin_info" | jq -r --arg f "$field" '.[$f] // empty')
			m=$(printf '%s' "$result" | jq -r --arg f "$field" '.[$f] // empty')
			[ "$a" != "$m" ] && mismatch="$mismatch $field(admin=$a monitor=$m)"
		done
		a=$(printf '%s' "$admin_info" | jq -r '.ports.listener // empty')
		m=$(printf '%s' "$result" | jq -r '.ports.listener // empty')
		[ "$a" != "$m" ] && mismatch="$mismatch ports.listener(admin=$a monitor=$m)"

		if [ -z "$mismatch" ]; then
			ok "monitor_nodeInfo agrees with admin_nodeInfo on every shared field"
		else
			bad "monitor_nodeInfo agrees with admin_nodeInfo" "differs:$mismatch"
		fi
	fi
fi
echo

# ---------------------------------------------------------------------------
echo "=== $pass passed, $fail failed, $skip skipped ==="
[ "$fail" -eq 0 ] || exit 1
