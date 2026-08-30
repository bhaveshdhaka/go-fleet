#!/usr/bin/env bash
# C22e — `fleet mcp` MUTATION refusals + legal paths (WO-22 phase 2).
# The pre-committed acceptance (JOURNEYS.md / MCP-BRIEF §6): every
# mutation surface ships with journeys proving the illegal paths are
# REFUSED with the exact fix before proving the legal path works.
#
#   SESSION 1 (default, read-only):
#     J1 — mutation tools are INVISIBLE in tools/list
#     J2 — calling a mutation tool is a JSON-RPC protocol error
#   SESSION 2 (--mutations via FLEET_MUTATIONS=1, actor=agent):
#     J3 — the five mutation tools are listed
#     J4 — prod approval as actor 'agent': actor-policy refusal + fix
#     J5 — prod approval as allowed actor: APPROVED (legal path works)
#     J6 — promote of an unknown component: refused, well-formed
#     J7 — ops_build of an unknown service: refused, no cluster touched
#
# Hermetic: tar-copy repo (real registry + policy), fake kubectl.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

jsonq_build "$scratch/jsonq" || { report_fail "jsonq builds" "go build failed"; finalize; }
J="$scratch/jsonq"

# fake kubectl: ops mutations must never reach a real cluster here
mkdir -p "$scratch/bin"
printf '#!/usr/bin/env bash\necho "unexpected: $*" >&2\nexit 97\n' > "$scratch/bin/kubectl"
chmod +x "$scratch/bin/kubectl"

repo="$scratch/repo"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist --exclude=vendor -cf - . | tar -C "$repo" -xf -
rm -f "$repo"/lifecycle/approvals/dev/*.approved "$repo"/lifecycle/approvals/prod/*.approved

source "$FLEET_ROOT/tests/lib/journey.sh"
J_SCRATCH="$scratch"; J_JQ="$J"
export PATH="$scratch/bin:$PATH" FLEET_ROOT="$repo"

MUTATIONS="fleet_approve fleet_promote ops_build ops_deploy ops_rollback"

# --- SESSION 1: default (read-only) server ---------------------------
journey_session_open "$F" "$repo"
journey_handshake || report_fail "handshake" "no response"
journey_send '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
journey_recv 2 > "$scratch/resp-2.json"
invisible=0
for t in $MUTATIONS; do
  grep -q "\"name\":\"$t\"" "$scratch/resp-2.json" && invisible=$((invisible + 1))
done
assert_eq "J1: default server lists zero mutation tools" "0" "$invisible"
journey_send '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ops_deploy","arguments":{"service":"alpha"}}}'
journey_recv 3 > "$scratch/resp-3.json"
grep -q '"error"' "$scratch/resp-3.json" \
  && report_pass "J2: calling an unregistered mutation tool is a protocol error" \
  || report_fail "J2: calling an unregistered mutation tool is a protocol error" "expected JSON-RPC error"
journey_session_close

# --- SESSION 2: mutations explicitly enabled, actor = agent ----------
export FLEET_MUTATIONS=1 FLEET_ACTOR=agent
journey_session_open "$F" "$repo"
journey_handshake || report_fail "mutations handshake" "no response"
journey_send '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
journey_recv 2 > "$scratch/resp-2b.json"
visible=0
for t in $MUTATIONS; do
  grep -q "\"name\":\"$t\"" "$scratch/resp-2b.json" && visible=$((visible + 1))
done
assert_eq "J3: mutations server lists all five mutation tools" "5" "$visible"

# J4 — prod approval as agent: actor-policy refusal with exact fix
journey_send '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fleet_approve","arguments":{"component":"fleetctl","stage":"prod"}}}'
journey_recv 4 > "$scratch/resp-4.json"
if journey_is_error 4 && journey_match 4 .result.content.0.text "may not approve stage 'prod'"; then
  report_pass "J4: prod approval as agent refused by actor policy (fix included)"
else
  report_fail "J4: prod approval as agent refused by actor policy (fix included)" "$(head -c 200 "$scratch/resp-4.json")"
fi

# J5 — prod approval as an allowed actor: APPROVED
journey_send '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"fleet_approve","arguments":{"component":"fleetctl","stage":"prod","who":"owner-via-agent"}}}'
journey_recv 5 > "$scratch/resp-5.json"
if ! journey_is_error 5 && journey_match 5 .result.content.0.text "APPROVED component=fleetctl stage=prod"; then
  report_pass "J5: allowed actor approves prod through MCP"
else
  report_fail "J5: allowed actor approves prod through MCP" "$(head -c 200 "$scratch/resp-5.json")"
fi
assert_file "J5: approval file landed in the repo" "$repo/lifecycle/approvals/prod/fleetctl.approved"

# J6 — promote of an unknown component: refused, well-formed
journey_send '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"fleet_promote","arguments":{"component":"ghost","stage":"dev"}}}'
journey_recv 6 > "$scratch/resp-6.json"
if journey_well_formed 6 && grep -q '"isError":true' "$scratch/resp-6.json"; then
  report_pass "J6: unknown-component promote refused cleanly"
else
  report_fail "J6: unknown-component promote refused cleanly" "$(head -c 160 "$scratch/resp-6.json")"
fi

# J7 — ops_build of an unknown service: refused, well-formed
journey_send '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"ops_build","arguments":{"service":"nosuch"}}}'
journey_recv 7 > "$scratch/resp-7.json"
if journey_well_formed 7 && journey_is_error 7; then
  report_pass "J7: unknown-service build refused cleanly (no cluster touched)"
else
  report_fail "J7: unknown-service build refused cleanly (no cluster touched)" "$(head -c 160 "$scratch/resp-7.json")"
fi

journey_session_close

finalize
