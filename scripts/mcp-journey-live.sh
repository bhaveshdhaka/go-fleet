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
OUT="$scratch/out"
JQ="$scratch/jsonq"

# jsonq (stdlib-only) for payload assertions
GO_BIN="${FLEET_TOOLCHAIN_PREFIX:-}/bin/go"
[[ -x "$GO_BIN" ]] || GO_BIN="$(command -v go)"
( cd "$FLEET_ROOT" && GOPROXY=off GOFLAGS=-mod=vendor GOTOOLCHAIN=local CGO_ENABLED=0 \
    "$GO_BIN" build -trimpath -o "$JQ" tests/lib/jsonq.go ) || {
  echo "LIVE-JOURNEY FAIL reason=jsonq-build"; exit 1; }

fifo="$scratch/in"
mkfifo "$fifo"
"$BIN" mcp < "$fifo" > "$OUT" 2> "$scratch/err" &
MCP_PID=$!
exec 9>"$fifo"

send() { printf '%s\n' "$1" >&9; }
recv_for() {
  local want="$1" deadline=$((SECONDS + 30)) cand
  while (( SECONDS < deadline )); do
    cand="$(grep -a -o "[{]\"jsonrpc\":\"2.0\",\"id\":$want,.*" "$OUT" 2>/dev/null | head -1)"
    if [[ -n "$cand" ]]; then
      printf '%s' "$cand" > "$scratch/cand.json"
      if "$JQ" "$scratch/cand.json" valid > /dev/null 2>&1; then
        printf '%s' "$cand"
        return 0
      fi
    fi
    sleep 0.2
  done
  return 1
}
call() { # id tool args
  send "{\"jsonrpc\":\"2.0\",\"id\":$1,\"method\":\"tools/call\",\"params\":{\"name\":\"$2\",\"arguments\":$3}}"
  recv_for "$1" > "$scratch/resp-$1.json"
}

rc=0
step() { # name ok
  if [[ "$2" == "0" ]]; then printf 'LIVE-JOURNEY PASS %s\n' "$1"
  else printf 'LIVE-JOURNEY FAIL %s\n' "$1"; rc=1; fi
}

send '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"live-journey","version":"0"}}}'
recv_for 1 > "$scratch/resp-1.json"; step "handshake" "$?"
send '{"jsonrpc":"2.0","method":"notifications/initialized"}'

# J1 morning triage on the LIVE estate
call 2 fleet_status '{}'
n="$( "$JQ" "$scratch/resp-2.json" len .result.structuredContent.components 2>/dev/null )"
[[ "${n:-0}" -ge 1 ]]; step "J1 live status answers (components=$n)" "$?"

call 3 fleet_doctor '{}'
ok="$( "$JQ" "$scratch/resp-3.json" str .result.structuredContent.ok 2>/dev/null )"
[[ "$ok" == "true" ]]; step "J1 live doctor ok=true (estate ALL CLEAR)" "$?"

call 4 fleet_next '{}'
a="$( "$JQ" "$scratch/resp-4.json" str .result.structuredContent.action 2>/dev/null )"
[[ -n "$a" ]]; step "J1 live next gives an action ($a)" "$?"

# J2 context assembly on the LIVE workorders + journal
call 5 fleet_wo_show '{"id":"WO-22"}'
"$JQ" "$scratch/resp-5.json" match .result.content.0.text "WO-22" > /dev/null 2>&1
step "J2 live wo show returns the MCP workorder" "$?"

send '{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"fleet://lifecycle/journal"}}'
recv_for 6 > "$scratch/resp-6.json"
"$JQ" "$scratch/resp-6.json" match .result.contents.0.text "verify" > /dev/null 2>&1
step "J2 live journal resource carries verify history" "$?"

# J3 the estate's site observation answers (may report cluster reachable)
call 7 ops_status '{}'
grep -q '"id":7' "$OUT" && grep -q 'structuredContent\|isError' "$scratch/resp-7.json"
step "J3 live ops_status answers (JSON or honest isError)" "$?"

exec 9>&-
wait "$MCP_PID" 2>/dev/null
[[ "$?" -eq 0 ]]; step "clean EOF exit" "$?"

[[ "$rc" -eq 0 ]] && echo "LIVE-JOURNEY OK" || echo "LIVE-JOURNEY FAIL"
exit "$rc"
