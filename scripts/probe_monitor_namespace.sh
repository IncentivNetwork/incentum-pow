#!/bin/bash
#
# probe_monitor_namespace.sh — verify the read-only monitor RPC namespace on a
# LIVE node.
#
# The namespace exists so monitoring can be served without exposing admin. This
# script checks that it is enabled and that it answers with the documented
# payload — values and types, not just keys — and it treats a still-reachable
# admin namespace as a failure rather than a note, since that is the condition
# the namespace was introduced to remove. Where admin is reachable it also
# reports whether monitor agrees with the readings it replaced.
#
# Both monitor methods are read-only and cheap, so this is safe to run against a
# production node at any time; no method it calls changes node state. Nothing
# identifying is printed, so a run can be attached to a public discussion.
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

# Nothing identifying is printed: the endpoint can carry credentials, and the
# chain id and client version are exactly the metadata that must not travel with
# an attached run. The result of each check is enough to act on.
echo "=== Probing the configured endpoint ==="
echo

# ---------------------------------------------------------------------------
if [ -z "$(call eth_chainId | jq -r '.result // empty')" ]; then
	echo "  the endpoint did not answer eth_chainId; check that it is reachable" >&2
	exit 2
fi

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

# The namespace exists so that monitoring can be served without admin. A probe
# that passed while admin was still reachable would not be checking that, so
# this is a failed security condition rather than advice.
admin_exposed=0
if printf '%s' "$modules" | jq -e 'has("admin")' >/dev/null 2>&1; then
	admin_exposed=1
	bad "admin is not exposed" \
		"admin is served here too; monitoring no longer needs it, and it grants control of the node"
else
	ok "admin is not exposed"
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

	# Presence is not enough. A live node can answer with the right keys and
	# useless contents — null, an empty string, port 0 — and a monitoring stack
	# would store that as if it were a reading. Each field is checked for its
	# JSON type and for a value that could actually have come from a running
	# node.
	check_field() { # label, jq predicate
		if printf '%s' "$result" | jq -e "$2" >/dev/null 2>&1; then
			ok "$1"
		else
			bad "$1" "$(printf '%s' "$result" | jq -c '{name,ip,listenAddr,ports,network,difficulty}')"
		fi
	}

	check_field "nodeInfo.name is a non-empty string" \
		'(.name | type) == "string" and (.name | length) > 0'
	check_field "nodeInfo.ip is a non-empty string" \
		'(.ip | type) == "string" and (.ip | length) > 0'
	check_field "nodeInfo.listenAddr is a non-empty string" \
		'(.listenAddr | type) == "string" and (.listenAddr | length) > 0'
	# A node that is actually listening reports a real port, not 0.
	check_field "nodeInfo.ports.listener is a port number" \
		'(.ports.listener | type) == "number" and .ports.listener > 0 and .ports.listener < 65536'
	# Discovery may legitimately be off, so only the type is required.
	check_field "nodeInfo.ports.discovery is a number" \
		'(.ports.discovery | type) == "number" and .ports.discovery >= 0'
	check_field "nodeInfo.network is a positive number" \
		'(.network | type) == "number" and .network > 0'
	# difficulty is a *big.Int, which marshals as a bare JSON number. A
	# monitoring stack parsing it as one breaks if that ever becomes a string.
	check_field "nodeInfo.difficulty is a non-negative JSON number" \
		'(.difficulty | type) == "number" and .difficulty >= 0'
fi
echo

# ---------------------------------------------------------------------------
echo "--- monitor_peerCount ---"
peers=$(call monitor_peerCount)
peer_count=$(printf '%s' "$peers" | jq -r 'if (.result | type) == "number" and .result >= 0 then .result else empty end')
if [ -z "$peer_count" ]; then
	bad "monitor_peerCount answers with a non-negative number" "$peers"
else
	ok "monitor_peerCount answers with a non-negative number ($peer_count)"
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
