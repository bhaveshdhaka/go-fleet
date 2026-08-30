# tests/lib/journey.sh — shared journey-session plumbing (WO-22 use-case
# tier, now a PERMANENT corpus tier: AGENTS.md rule 9).
#
# Journeys are user stories driven through the REAL fleet mcp stdio
# surface — morning triage, incident drills, refusals, context assembly,
# client batching — NOT API probes. Every feature ships its journeys in
# the same change; the corpus runs them every time.
#
# Usage (in a journey unit, AFTER `source scripts/lib.sh`):
#   J_SCRATCH="$scratch"; jsonq_build "$scratch/jsonq"; J_JQ="$scratch/jsonq"
#   journey_session_open "$F" "$repo"          # repo="" -> inherit FLEET_ROOT
#   journey_call 2 fleet_status '{}'           # -> $J_SCRATCH/resp-2.json
#   journey_resource 13 fleet://lifecycle/journal
#   journey_struct 2 .result.structuredContent.ok
#   journey_is_error 5 ; journey_match 12 .result.content.0.text "# WO-1"
#   journey_session_close                       # asserts clean EOF + quiet stderr
#
# Contract: one fleet mcp stdio session per unit; responses are polled by
# id and accepted only once jsonq parses them WHOLE (the SDK does not
# line-flush stdout — response bytes appear incrementally; a line-gated
# reader starves. Measured, asserted by C22a).

# Reporting: units source scripts/lib.sh (report_pass/fail land in the
# corpus report). Non-corpus drivers (scripts/mcp-journey-live.sh) get a
# plain fallback so the same close-asserts work everywhere.
if ! declare -F report_pass > /dev/null; then
  report_pass() { printf 'JOURNEY PASS %s\n' "$1"; }
  report_fail() { printf 'JOURNEY FAIL %s :: %s\n' "$1" "${2:-}"; }
fi

# session state (all J_* so units never collide with lib.sh vars)
J_OUT=""; J_FIFO=""; J_PID=""; J_JQ=""; J_SCRATCH=""; J_ERR=""
J_RECV_TIMEOUT="${J_RECV_TIMEOUT:-20}"

journey_session_open() { # <binary> <repo-root-or-""> [label]
  local bin="$1" repo="$2" label="${3:-journey}"
  J_OUT="$J_SCRATCH/out"; J_ERR="$J_SCRATCH/err"; J_FIFO="$J_SCRATCH/in"
  mkfifo "$J_FIFO"
  if [[ -n "$repo" ]]; then
    FLEET_ROOT="$repo" "$bin" mcp < "$J_FIFO" > "$J_OUT" 2> "$J_ERR" &
  else
    "$bin" mcp < "$J_FIFO" > "$J_OUT" 2> "$J_ERR" &
  fi
  J_PID=$!
  exec 9>"$J_FIFO"
}

journey_send() { printf '%s\n' "$1" >&9; }

journey_recv() { # <id> -> echoes the COMPLETE response line (jsonq-valid gate)
  local want="$1" deadline=$((SECONDS + J_RECV_TIMEOUT)) cand
  while (( SECONDS < deadline )); do
    cand="$(grep -a -o "[{]\"jsonrpc\":\"2.0\",\"id\":$want,.*" "$J_OUT" 2>/dev/null | head -1)"
    if [[ -n "$cand" ]]; then
      printf '%s' "$cand" > "$J_SCRATCH/cand.json"
      if "$J_JQ" "$J_SCRATCH/cand.json" valid > /dev/null 2>&1; then
        printf '%s' "$cand"
        return 0
      fi
    fi
    sleep 0.2
  done
  return 1
}

journey_call() { # <id> <tool> [arguments-json]
  local id="$1" tool="$2" args="${3:-{\}}"
  journey_send "{\"jsonrpc\":\"2.0\",\"id\":$id,\"method\":\"tools/call\",\"params\":{\"name\":\"$tool\",\"arguments\":$args}}"
  journey_recv "$id" > "$J_SCRATCH/resp-$id.json"
}

journey_resource() { # <id> <uri>
  journey_send "{\"jsonrpc\":\"2.0\",\"id\":$1,\"method\":\"resources/read\",\"params\":{\"uri\":\"$2\"}}"
  journey_recv "$1" > "$J_SCRATCH/resp-$1.json"
}

journey_handshake() { # initialize + initialized notification
  journey_send '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"journey","version":"0"}}}'
  journey_recv 1 > "$J_SCRATCH/resp-1.json"
  journey_send '{"jsonrpc":"2.0","method":"notifications/initialized"}'
  [[ -s "$J_SCRATCH/resp-1.json" ]]
}

journey_struct() { # <id> <jsonq-path> -> prints scalar ("" on miss)
  "$J_JQ" "$J_SCRATCH/resp-$1.json" str "$2" 2>/dev/null
}

journey_len() { # <id> <jsonq-path> -> prints length (-1 on miss)
  "$J_JQ" "$J_SCRATCH/resp-$1.json" len "$2" 2>/dev/null
}

journey_match() { # <id> <jsonq-path> <substring> -> rc 0 if contained
  "$J_JQ" "$J_SCRATCH/resp-$1.json" match "$2" "$3" > /dev/null 2>&1
}

journey_is_error() { # <id> -> rc 0 if the tool result is isError
  grep -q '"isError":true' "$J_SCRATCH/resp-$1.json" 2>/dev/null
}

journey_well_formed() { # <id> -> rc 0 if the answer is a usable result:
  # isError with text, or structuredContent, or non-empty text content.
  # The NEVER-empty-NEVER-crash invariant of the whole surface.
  local f="$J_SCRATCH/resp-$1.json"
  [[ -s "$f" ]] || return 1
  if journey_is_error "$1"; then
    "$J_JQ" "$f" match .result.content.0.text "" > /dev/null 2>&1 && return 0
    # match with empty needle may be refused; fall back to a parse check
    "$J_JQ" "$f" valid > /dev/null 2>&1 && return 0
    return 1
  fi
  "$J_JQ" "$f" has .result.structuredContent x > /dev/null 2>&1 && return 0
  [[ -n "$(journey_struct "$1" .result.content.0.text)" ]] && return 0
  return 1
}

journey_session_close() { # clean EOF exit + quiet stderr (two asserts)
  exec 9>&- 2>/dev/null
  wait "$J_PID" 2>/dev/null
  local src=$?
  [[ "$src" -eq 0 ]] \
    && report_pass "server exits cleanly on stdin EOF" \
    || report_fail "server exits cleanly on stdin EOF" "exit=$src"
  if [[ -s "$J_ERR" ]]; then
    report_fail "server stderr is quiet" "$(head -3 "$J_ERR")"
  else
    report_pass "server stderr is quiet"
  fi
}
