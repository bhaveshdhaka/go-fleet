#!/usr/bin/env bash
# C22d — journey-tier INTEGRITY gate (WO-22 use-case tier; AGENTS rule 9).
# The journey tier is a PERMANENT product surface: this unit asserts the
# structure that keeps it honest —
#   1. the shared journey library exists and is the ONLY session plumbing
#      (one recv loop repo-wide; no per-unit drift),
#   2. every journey unit + the live driver source it,
#   3. the doctrine exists (lifecycle/JOURNEYS.md) with the inventory,
#   4. AGENTS.md carries the rule (features ship journeys in the same
#      change; mutations ship refusal journeys).

source "$FLEET_ROOT/scripts/lib.sh"

LIB="$FLEET_ROOT/tests/lib/journey.sh"
DOC="$FLEET_ROOT/lifecycle/JOURNEYS.md"

# 1. the library is the single session plumbing
assert_file "journey library present" "$LIB"
n_recv="$(grep -c 'journey_recv()' "$LIB" 2>/dev/null)"
assert_eq "recv loop defined exactly once (in the library)" "1" "$n_recv"
# (exclude this gate's own pattern literal, C20d-style)
dupes="$(grep -rln 'recv_for()' "$FLEET_ROOT/tests" "$FLEET_ROOT/scripts" 2>/dev/null \
  | grep -v "lib.sh\|C22d_journey_tier_gate" || true)"
if [[ -z "$dupes" ]]; then
  report_pass "no per-unit session plumbing drift (no recv_for copies)"
else
  report_fail "no per-unit session plumbing drift (no recv_for copies)" "$dupes"
fi

# 2. every journey unit + the live driver source the library
for u in C22a_mcp_stdio C22b_mcp_secret_guard C22c_mcp_operator_journeys; do
  f="$FLEET_ROOT/tests/$u/run.sh"
  assert_file "journey unit present: $u" "$f"
  assert_contains "unit sources the journey library: $u" \
    'source "$FLEET_ROOT/tests/lib/journey.sh"' "$(cat "$f")"
done
assert_contains "live journey driver sources the library" \
  'source "$FLEET_ROOT/tests/lib/journey.sh"' \
  "$(cat "$FLEET_ROOT/scripts/mcp-journey-live.sh")"

# 3. doctrine + inventory
assert_file "journey doctrine present (lifecycle/JOURNEYS.md)" "$DOC"
assert_contains "doctrine inventories the wire-contract journeys" "C22a" "$(cat "$DOC")"
assert_contains "doctrine inventories the secret-guard journey" "C22b" "$(cat "$DOC")"
assert_contains "doctrine inventories the operator journeys" "C22c" "$(cat "$DOC")"
assert_contains "doctrine inventories the live-estate tier" "mcp-journey-live" "$(cat "$DOC")"

# 4. the law carries the rule
assert_contains "AGENTS.md rule 9 (journeys ship with features)" \
  "No journey, no ship" "$(cat "$FLEET_ROOT/AGENTS.md")"
assert_contains "AGENTS.md rule 9 (refusal journeys for mutations)" \
  "refusal journeys" "$(cat "$FLEET_ROOT/AGENTS.md")"

finalize
