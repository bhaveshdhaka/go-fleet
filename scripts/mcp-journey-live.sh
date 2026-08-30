#!/usr/bin/env bash
# scripts/mcp-journey-live.sh — TIER-2 evidence: drive `fleet mcp` through
# an operator's read-only morning against the REAL repo this script ships
# in. This is the bridge between hermetic corpus journeys (C22c, fixture)
# and a real client session: same wire, same tools, LIVE answers.
#
# STRICTLY READ-ONLY: the five verbs it calls are corpus-pinned read-only
# (C22a asserts no mutating verb is reachable). No cluster mutations, no
# Cloudflare calls, no state writes — the only fleet-side writer it may
# touch is the journal, and it does NOT even do that (append the finding
# yourself with `./scripts/fleet`-adjacent discipline if you journal it).
#
# Usage: scripts/mcp-journey-live.sh   (exit 0 = every journey answered)

set -uo pipefail
FLEET_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export FLEET_ROOT
BIN="$FLEET_ROOT/dist/fleet"
[[ -x "$BIN" ]] || { echo "LIVE-JOURNEY FAIL reason=binary-missing (run ci/build-fleet.sh)"; exit 1; }

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
JQ="$scratch/jsonq"

# jsonq (stdlib-only) for payload assertions
GO_BIN="${FLEET_TOOLCHAIN_PREFIX:-}/bin/go"
[[ -x "$GO_BIN" ]] || GO_BIN="$(command -v go)"
( cd "$FLEET_ROOT" && GOPROXY=off GOFLAGS=-mod=vendor GOTOOLCHAIN=local CGO_ENABLED=0 \
    "$GO_BIN" build -trimpath -o "$JQ" tests/lib/jsonq.go ) || {
  echo "LIVE-JOURNEY FAIL reason=jsonq-build"; exit 1; }

# shared journey session plumbing (same code the corpus journeys run)
source "$FLEET_ROOT/tests/lib/journey.sh"
J_SCRATCH="$scratch"; J_JQ="$JQ"; J_RECV_TIMEOUT=30
journey_session_open "$BIN" ""   # repo="" -> resolve FLEET_ROOT from cwd/defaults

rc=0
step() { # name ok
  if [[ "$2" == "0" ]]; then printf 'LIVE-JOURNEY PASS %s\n' "$1"
  else printf 'LIVE-JOURNEY FAIL %s\n' "$1"; rc=1; fi
}

journey_handshake; step "handshake" "$?"

# J1 morning triage on the LIVE estate
journey_call 2 fleet_status '{}'
n="$( "$JQ" "$scratch/resp-2.json" len .result.structuredContent.components 2>/dev/null )"
[[ "${n:-0}" -ge 1 ]]; step "J1 live status answers (components=$n)" "$?"

journey_call 3 fleet_doctor '{}'
ok="$( "$JQ" "$scratch/resp-3.json" str .result.structuredContent.ok 2>/dev/null )"
[[ "$ok" == "true" ]]; step "J1 live doctor ok=true (estate ALL CLEAR)" "$?"

journey_call 4 fleet_next '{}'
a="$( "$JQ" "$scratch/resp-4.json" str .result.structuredContent.action 2>/dev/null )"
[[ -n "$a" ]]; step "J1 live next gives an action ($a)" "$?"

# J2 context assembly on the LIVE workorders + journal
journey_call 5 fleet_wo_show '{"id":"WO-22"}'
"$JQ" "$scratch/resp-5.json" match .result.content.0.text "WO-22" > /dev/null 2>&1
step "J2 live wo show returns the MCP workorder" "$?"

journey_send '{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"fleet://lifecycle/journal"}}'
journey_recv 6 > "$scratch/resp-6.json"
"$JQ" "$scratch/resp-6.json" match .result.contents.0.text "verify" > /dev/null 2>&1
step "J2 live journal resource carries verify history" "$?"

# J3 the estate's site observation answers (may report cluster reachable)
journey_call 7 ops_status '{}'
[[ -s "$scratch/resp-7.json" ]] && journey_well_formed 7
step "J3 live ops_status answers (JSON or honest isError)" "$?"

journey_session_close
rc_last=$?
[[ "$rc_last" -eq 0 ]]; step "clean EOF exit + quiet stderr" "$rc_last"

[[ "$rc" -eq 0 ]] && echo "LIVE-JOURNEY OK" || echo "LIVE-JOURNEY FAIL"
exit "$rc"
