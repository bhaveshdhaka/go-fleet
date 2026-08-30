#!/usr/bin/env bash
# C22c — `fleet mcp` OPERATOR JOURNEYS (WO-22 phase 1, use-case tier).
# NOT a protocol test — every journey here is a user story an operator
# (or their agent) actually lives, driven through the same stdio surface
# a real client uses:
#
#   JOURNEY 1 — morning triage:      "is everything OK, and what's next?"
#   JOURNEY 2 — incident drill:      break state mid-session; the agent
#                must DETECT (doctor ok:false), PINPOINT (issue text),
#                GET ROUTED (next -> ./scripts/fleet doctor), fix, and
#                re-verify — the full detect->fix loop.
#   JOURNEY 3 — context assembly:    reconstruct enough history from wo
#                list/show + the journal resource to write a handoff.
#   JOURNEY 4 — pipelined batching:  a real client fires several calls
#                WITHOUT waiting (write-burst); every response must
#                still come back complete and correctly paired by id.
#   JOURNEY 5 — refusal UX:          an impossible question gets an
#                actionable error, not a hang or a crash.
#
# Hermetic: fixture repo, fake-free (no cluster, no network).

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

jsonq_build "$scratch/jsonq" || { report_fail "jsonq builds" "go build failed"; finalize; }
J="$scratch/jsonq"

# --- fixture: a small but HONEST repo (registry paths exist on disk, so
# doctor is genuinely clean; stages legal; journal all-findings)
repo="$scratch/repo"
mkdir -p "$repo/ops/state" "$repo/lifecycle/journal" "$repo/workorders" \
         "$repo/apps/alpha" "$repo/apps/beta" \
         "$repo/ci/pipelines" "$repo/infra/k8s"
: > "$repo/ci/pipelines/alpha.yaml"   # doctor stats these as files
: > "$repo/ci/pipelines/beta.yaml"

cat > "$repo/ops/PROJECTS.yaml" <<'EOF'
registry_version: 1

components:

  - name: alpha
    kind: cli
    path: apps/alpha
    pipeline: ci/pipelines/alpha.yaml
    manifests: infra/k8s
    description: fixture cli
    enabled: true

  - name: beta
    kind: service
    path: apps/beta
    pipeline: ci/pipelines/beta.yaml
    manifests: infra/k8s
    description: fixture service
    enabled: true
EOF

cat > "$repo/ops/state/deployments.yaml" <<'EOF'
state_version: 1

components:

  - name: alpha
    stage: dev

  - name: beta
    stage: built
EOF

cat > "$repo/lifecycle/gates.yaml" <<'EOF'
gates_version: 1

gates: {}
EOF

cat > "$repo/lifecycle/journal/events.log" <<'EOF'
# c22c fixture: seed history
# verify ts=2026-08-29T00:00:00Z wo=WO-2 units=1 pass=1 fail=0 skip=0 result=OK
# promote component=beta from=built to=dev actor=agent
EOF

cat > "$repo/workorders/WO-2.md" <<'EOF'
---
wo: WO-2
title: fixture workorder
status: OPEN
plan: PLAN.md
pieces:
  - id: 1
    title: fixture piece
    verify: tests/C1a
    integrated: false
---

# WO-2 — fixture workorder

Body text for the context-assembly journey.
EOF

# --- session plumbing (same contract as C22a/C22b)
fifo="$scratch/in"
OUT="$scratch/out"
mkfifo "$fifo"
FLEET_ROOT="$repo" "$F" mcp < "$fifo" > "$OUT" 2> "$scratch/err" &
MCP_PID=$!
exec 9>"$fifo"

send() { printf '%s\n' "$1" >&9; }
recv_for() { # $1=id -> complete response line (grep + jsonq-valid gate)
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
call() { # $1=id $2=tool $3=arguments-json
  send "{\"jsonrpc\":\"2.0\",\"id\":$1,\"method\":\"tools/call\",\"params\":{\"name\":\"$2\",\"arguments\":$3}}"
  recv_for "$1" > "$scratch/resp-$1.json"
}

send '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c22c-journeys","version":"0"}}}'
recv_for 1 > "$scratch/resp-1.json" || report_fail "initialize" "no response"
send '{"jsonrpc":"2.0","method":"notifications/initialized"}'

# JOURNEY 1 — morning triage: status -> doctor -> next
# user story: "I just sat down. Is anything broken? What am I supposed to do?"
call 2 fleet_status '{}'
st="$scratch/resp-2.json"
"$J" "$st" str .result.structuredContent.components.0.stage > "$scratch/j1-alpha-stage" 2>/dev/null
assert_eq "J1: alpha observed at dev" "dev" "$(cat "$scratch/j1-alpha-stage")"
call 3 fleet_doctor '{}'
"$J" "$scratch/resp-3.json" str .result.structuredContent.ok > "$scratch/j1-ok" 2>/dev/null
assert_eq "J1: doctor says ok=true" "true" "$(cat "$scratch/j1-ok")"
call 4 fleet_next '{}'
action="$(cat "$scratch/resp-4.json" | "$J" /dev/stdin str .result.structuredContent.action 2>/dev/null)"
assert_eq "J1: next routes to the first legal hop" "./scripts/fleet approve alpha dev" "$action"

# JOURNEY 2 — incident drill: break beta's stage, then detect -> pinpoint
# -> get routed -> fix -> re-verify. The loop an operator actually runs.
sed -i 's/^    stage: built$/    stage: bogus/' "$repo/ops/state/deployments.yaml"
call 5 fleet_doctor '{}'
ok="$(cat "$scratch/resp-5.json" | "$J" /dev/stdin str .result.structuredContent.ok 2>/dev/null)"
assert_eq "J2: drill detected (doctor ok=false)" "false" "$ok"
call 6 fleet_next '{}'
action="$(cat "$scratch/resp-6.json" | "$J" /dev/stdin str .result.structuredContent.action 2>/dev/null)"
assert_eq "J2: next redirects to doctor first" "./scripts/fleet doctor" "$action"
call 7 fleet_doctor '{}'
"$J" "$scratch/resp-7.json" match .result.structuredContent.issues.0 "illegal stage 'bogus'" > /dev/null 2>&1 \
  && report_pass "J2: issue text pinpoints the broken component" \
  || report_fail "J2: issue text pinpoints the broken component" "pinpoint text missing"
# the fix (operator edits state through the ONLY legal writer here: fixture sed)
sed -i 's/^    stage: bogus$/    stage: built/' "$repo/ops/state/deployments.yaml"
call 8 fleet_doctor '{}'
ok="$(cat "$scratch/resp-8.json" | "$J" /dev/stdin str .result.structuredContent.ok 2>/dev/null)"
assert_eq "J2: after fix, doctor ok=true again" "true" "$ok"
call 9 fleet_next '{}'
action="$(cat "$scratch/resp-9.json" | "$J" /dev/stdin str .result.structuredContent.action 2>/dev/null)"
assert_eq "J2: after fix, next returns to the legal hop" "./scripts/fleet approve alpha dev" "$action"
call 10 fleet_status '{}'
stage="$(cat "$scratch/resp-10.json" | "$J" /dev/stdin find .result.structuredContent.components component=beta stage 2>/dev/null)"
assert_eq "J2: status agrees with doctor (no cross-tool drift)" "built" "$stage"

# JOURNEY 3 — context assembly: enough history for a handoff summary
call 11 fleet_wo_list '{}'
"$J" "$scratch/resp-11.json" match .result.content.0.text "WO id=WO-2 status=OPEN" > /dev/null 2>&1 \
  && report_pass "J3: wo list shows the open workorder" \
  || report_fail "J3: wo list shows the open workorder" "WO-2 line missing"
call 12 fleet_wo_show '{"id":"WO-2"}'
"$J" "$scratch/resp-12.json" match .result.content.0.text "Body text for the context-assembly journey" > /dev/null 2>&1 \
  && report_pass "J3: wo show returns the full brief" \
  || report_fail "J3: wo show returns the full brief" "body missing"
send '{"jsonrpc":"2.0","id":13,"method":"resources/read","params":{"uri":"fleet://lifecycle/journal"}}'
recv_for 13 > "$scratch/resp-13.json"
"$J" "$scratch/resp-13.json" match .result.contents.0.text "promote component=beta" > /dev/null 2>&1 \
  && report_pass "J3: journal resource carries the transition history" \
  || report_fail "J3: journal resource carries the transition history" "promote line missing"

# JOURNEY 4 — pipelined batching: fire three calls WITHOUT waiting
# (real clients pipeline); all three must return, complete, by id.
send '{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"fleet_status","arguments":{}}}'
send '{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"fleet_next","arguments":{}}}'
send '{"jsonrpc":"2.0","id":23,"method":"tools/call","params":{"name":"fleet_check","arguments":{}}}'
r21=0; r22=0; r23=0
recv_for 21 > "$scratch/resp-21.json" && r21=1
recv_for 22 > "$scratch/resp-22.json" && r22=1
recv_for 23 > "$scratch/resp-23.json" && r23=1
assert_eq "J4: all three pipelined calls answered" "3" "$((r21 + r22 + r23))"
ncomp="$(cat "$scratch/resp-21.json" | "$J" /dev/stdin len .result.structuredContent.components 2>/dev/null)"
assert_eq "J4: pipelined status payload intact" "2" "$ncomp"
npred="$(cat "$scratch/resp-23.json" | "$J" /dev/stdin len .result.structuredContent.predicates 2>/dev/null)"
[[ "$npred" -ge 6 ]] 2>/dev/null \
  && report_pass "J4: pipelined check payload intact (P1-P6)" \
  || report_fail "J4: pipelined check payload intact (P1-P6)" "predicates=$npred"

# JOURNEY 5 — refusal UX: impossible question, actionable answer
call 24 ops_status '{}'
grep -q '"isError":true' "$scratch/resp-24.json" \
  && report_pass "J5: impossible ask surfaces as isError" \
  || report_fail "J5: impossible ask surfaces as isError" "expected isError"
"$J" "$scratch/resp-24.json" match .result.content.0.text "sites registry unreadable" > /dev/null 2>&1 \
  && report_pass "J5: error text names the actual problem" \
  || report_fail "J5: error text names the actual problem" "$(head -c 160 "$scratch/resp-24.json")"

# shutdown: clean EOF exit
exec 9>&-
wait "$MCP_PID" 2>/dev/null
assert_eq "server exits cleanly on stdin EOF" "0" "$?"
[[ -s "$scratch/err" ]] && report_fail "server stderr is quiet" "$(head -3 "$scratch/err")" \
  || report_pass "server stderr is quiet"

finalize
