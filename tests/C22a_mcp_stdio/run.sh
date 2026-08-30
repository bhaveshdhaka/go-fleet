#!/usr/bin/env bash
# C22a — `fleet mcp` stdio contract (WO-22 phase 1).
# Hermetic: a minimal fixture root; the binary is driven over stdio with
# newline-delimited JSON-RPC. Asserts the READ-ONLY surface: exactly the
# 10 read tools (no mutating verb reachable), initialize handshake, one
# --json tools/call with structuredContent, one text tools/call, the
# resources list, journal resource read, and honest error mapping
# (refusals/failures come back as isError results, never as crashes).

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

# --- minimal fixture root (no sites registry -> ops verbs refuse cleanly)
repo="$scratch/repo"
mkdir -p "$repo/ops/state" "$repo/lifecycle/journal" "$repo/workorders"

cat > "$repo/ops/PROJECTS.yaml" <<'EOF'
registry_version: 1

components:

  - name: alpha
    kind: cli
    path: apps/alpha
    description: fixture cli
    enabled: true

  - name: beta
    kind: service
    path: apps/beta
    description: fixture service
    enabled: true
EOF

cat > "$repo/ops/state/deployments.yaml" <<'EOF'
state_version: 1

components:

  - name: alpha
    stage: dev
EOF

cat > "$repo/lifecycle/gates.yaml" <<'EOF'
gates_version: 1

gates: {}
EOF

printf '# c22a fixture journal seed\n' > "$repo/lifecycle/journal/events.log"
printf '# WO-1 — fixture workorder\n\nBody.\n' > "$repo/workorders/WO-1.md"

# --- jsonq for shape asserts
jsonq_build "$scratch/jsonq" || { report_fail "jsonq builds" "go build failed"; finalize; }
J="$scratch/jsonq"

# --- stdio session: hold stdin open; poll responses by id. The SDK does
# NOT line-flush stdout — response bytes appear incrementally and a
# message's trailing newline can lag behind its JSON. So: grep for the id
# prefix (grep matches an unterminated last line), then accept it only
# once jsonq parses it as complete JSON.
fifo="$scratch/in"
OUT="$scratch/out"
mkfifo "$fifo"
FLEET_ROOT="$repo" "$F" mcp < "$fifo" > "$OUT" 2> "$scratch/err" &
MCP_PID=$!
exec 9>"$fifo"

send() { printf '%s\n' "$1" >&9; }

recv_for() { # $1 = numeric request id -> echoes the complete response line
  local want="$1" deadline=$((SECONDS + 20)) cand
  while (( SECONDS < deadline )); do
    cand="$(grep -a -o "[{]\"jsonrpc\":\"2.0\",\"id\":$want,.*" "$OUT" 2>/dev/null | head -1)"
    if [[ -n "$cand" ]]; then
      printf '%s' "$cand" > "$scratch/cand.json"
      if "$J" "$scratch/cand.json" valid > /dev/null 2>&1; then
        printf '%s' "$cand"
        return 0
      fi
    fi
    sleep 0.2
  done
  return 1
}

save_resp() { recv_for "$1" > "$scratch/resp-$1.json"; }

send '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c22a","version":"0"}}}'
if save_resp 1; then report_pass "initialize responds"; else report_fail "initialize responds" "no response for id=1"; fi
send '{"jsonrpc":"2.0","method":"notifications/initialized"}'

name="$("$J" "$scratch/resp-1.json" str .result.serverInfo.name 2>/dev/null)"
assert_eq "serverInfo.name == fleet" "fleet" "$name"

send '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
if save_resp 2; then report_pass "tools/list responds"; else report_fail "tools/list responds" "no response for id=2"; fi
tl="$scratch/resp-2.json"

ntools="$("$J" "$tl" len .result.tools 2>/dev/null)"
assert_eq "exactly 10 tools exposed" "10" "$ntools"

grep -o '"name":"[^"]*"' "$tl" | sed 's/"name":"//;s/"$//' | sort > "$scratch/tool-names.txt"
cat > "$scratch/tool-names-want.txt" <<'EOF'
fleet_check
fleet_doctor
fleet_next
fleet_sites
fleet_status
fleet_wo_list
fleet_wo_show
ops_dns
ops_doctor
ops_status
EOF
if diff -u "$scratch/tool-names-want.txt" "$scratch/tool-names.txt" > "$scratch/tool-names.diff" 2>&1; then
  report_pass "tool set is exactly the read-only surface"
else
  report_fail "tool set is exactly the read-only surface" "$(cat "$scratch/tool-names.diff")"
fi

if grep -qE '"name":"(promote|approve|build|deploy|rollback|remove|monitor|register|update|apply)"' "$tl"; then
  report_fail "no mutating tool names" "mutation-looking name in tools/list"
else
  report_pass "no mutating tool names in tools/list"
fi

# --- fleet_status: --json truth flows through as structuredContent
send '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"fleet_status","arguments":{}}}'
if save_resp 3; then report_pass "tools/call fleet_status responds"; else report_fail "tools/call fleet_status responds" "no response for id=3"; fi
st="$scratch/resp-3.json"
ncomp="$("$J" "$st" len .result.structuredContent.components 2>/dev/null)"
assert_eq "fleet_status structuredContent carries components" "2" "$ncomp"
kind="$("$J" "$st" find .result.structuredContent.components component=alpha kind 2>/dev/null)"
assert_eq "component truth matches registry" "cli" "$kind"
"$J" "$st" str .result.content.0.type >/dev/null 2>&1 \
  && report_pass "fleet_status also carries text content" \
  || report_fail "fleet_status also carries text content" "no content block"

# --- fleet_next: action string present
send '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fleet_next","arguments":{}}}'
if save_resp 4; then
  action="$("$J" "$scratch/resp-4.json" str .result.structuredContent.action 2>/dev/null)"
  [[ -n "$action" ]] && report_pass "fleet_next returns an action" \
    || report_fail "fleet_next returns an action" "empty action"
else
  report_fail "fleet_next returns an action" "no response for id=4"
fi

# --- ops_status with no sites registry: honest isError, not a crash
send '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"ops_status","arguments":{}}}'
if save_resp 5; then
  grep -q '"isError":true' "$scratch/resp-5.json" \
    && report_pass "ops refusal surfaces as isError" \
    || report_fail "ops refusal surfaces as isError" "expected isError result"
else
  report_fail "ops refusal surfaces as isError" "no response for id=5"
fi

# --- text verb: wo show
send '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"fleet_wo_show","arguments":{"id":"WO-1"}}}'
if save_resp 6; then
  "$J" "$scratch/resp-6.json" match .result.content.0.text "# WO-1" >/dev/null 2>&1 \
    && report_pass "fleet_wo_show returns workorder text" \
    || report_fail "fleet_wo_show returns workorder text" "marker missing"
else
  report_fail "fleet_wo_show returns workorder text" "no response for id=6"
fi

# --- invalid workorder id: isError
send '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"fleet_wo_show","arguments":{"id":"../escape"}}}'
if save_resp 7; then
  grep -q '"isError":true' "$scratch/resp-7.json" \
    && report_pass "path traversal id refused as isError" \
    || report_fail "path traversal id refused as isError" "expected isError result"
else
  report_fail "path traversal id refused as isError" "no response for id=7"
fi

# --- resources
send '{"jsonrpc":"2.0","id":8,"method":"resources/list"}'
if save_resp 8; then
  nres="$("$J" "$scratch/resp-8.json" len .result.resources 2>/dev/null)"
  assert_eq "5 contract resources exposed" "5" "$nres"
else
  report_fail "5 contract resources exposed" "no response for id=8"
fi

send '{"jsonrpc":"2.0","id":9,"method":"resources/read","params":{"uri":"fleet://lifecycle/journal"}}'
if save_resp 9; then
  "$J" "$scratch/resp-9.json" match .result.contents.0.text "c22a fixture journal seed" >/dev/null 2>&1 \
    && report_pass "journal resource readable" \
    || report_fail "journal resource readable" "seed line missing"
else
  report_fail "journal resource readable" "no response for id=9"
fi

send '{"jsonrpc":"2.0","id":10,"method":"resources/read","params":{"uri":"fleet://registry/unknown"}}'
if save_resp 10; then
  grep -q '"error"' "$scratch/resp-10.json" \
    && report_pass "unknown resource URI is a JSON-RPC error" \
    || report_fail "unknown resource URI is a JSON-RPC error" "expected error object"
else
  report_fail "unknown resource URI is a JSON-RPC error" "no response for id=10"
fi

# --- shutdown: close stdin; server must exit cleanly (EOF, not crash)
exec 9>&-
wait "$MCP_PID" 2>/dev/null
src=$?
assert_eq "server exits cleanly on stdin EOF" "0" "$src"

[[ -s "$scratch/err" ]] && report_fail "server stderr is quiet" "$(head -3 "$scratch/err")" \
  || report_pass "server stderr is quiet"

finalize
