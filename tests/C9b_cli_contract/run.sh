#!/usr/bin/env bash
# C9b — Go core CLI contract end-to-end inside an ISOLATED REPO COPY.
# Drives the control plane through the same paths agents use (scripts/fleet
# shim) and asserts: status/doctor machine lines, promote refusals, dry-run
# byte-stability, gated promotion with unit re-run, journal + state
# discipline, idempotent repeats. Nothing outside the scratch copy mutates.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
repo="$scratch/fleet"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo" -xf -
# isolation: pristine baseline (C5c contract)
sed -i -E 's/^(    stage: ).*/\1built/; s/^(    last_promoted_at: ).*/\1""/' \
  "$repo/ops/state/deployments.yaml"
rm -f "$repo"/lifecycle/approvals/dev/*.approved "$repo"/lifecycle/approvals/prod/*.approved
sed -i '/^ts=/d' "$repo/lifecycle/journal/events.log"

f() { FLEET_ROOT="$repo" bash "$repo/scripts/fleet" "$@"; }
hash_tree() { (cd "$repo" && find ops lifecycle ci scripts tests -type f -print0 2>/dev/null | sort -z | xargs -0 -r sha256sum | sha256sum); }

o="$(f status)"
assert_contains "status lists fleetctl" "STATUS component=fleetctl kind=cli stage=built" "$o"
assert_contains "status lists fleethub" "STATUS component=fleethub kind=service stage=built" "$o"
assert_contains "status summary" "STATUS SUMMARY components=2" "$o"

o="$(f doctor)"
assert_contains "doctor OK on baseline" "DOCTOR OK checked_components=2 issues=0" "$o"

d1="$(f promote fleetctl dev --dry-run 2>&1)"; r1=$?
d2="$(f promote fleetctl dev --dry-run 2>&1)"
{ [[ $r1 -eq 0 && "$d1" == *"[promote][dry-run] would move fleetctl: built -> dev" ]]; } \
  && report_pass "dry-run contract line" || report_fail "dry-run contract line" "$r1 :: $d1"
assert_eq "dry-run byte-identical" "$d1" "$d2"
h0=$(hash_tree "$repo")

o="$(f promote fleetctl prod --dry-run 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"illegal transition"* ]]; } \
  && report_pass "non-adjacent hop refused" || report_fail "non-adjacent hop refused" "$rc :: $o"
o="$(f promote ghost built 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"unknown component 'ghost'"* ]]; } \
  && report_pass "unknown component refused" || report_fail "unknown component refused" "$rc :: $o"
o="$(f promote fleetctl bogus 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"illegal target stage 'bogus'"* ]]; } \
  && report_pass "illegal stage refused" || report_fail "illegal stage refused" "$rc :: $o"
o="$(f promote fleetctl dev --bogus 2>&1)"; rc=$?
[[ $rc -eq 2 ]] && report_pass "unknown flag rc=2" || report_fail "unknown flag rc=2" "$rc"
o="$(f approve fleetctl 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == "usage: fleet approve <component> <dev|prod> [approver]" ]]; } \
  && report_pass "approve usage error exact" || report_fail "approve usage error exact" "$rc :: $o"

assert_eq "refusals mutate nothing" "$h0" "$(hash_tree "$repo")"

o="$(f promote fleetctl dev 2>&1)"; rc=$?
{ [[ $rc -eq 0 && "$o" == "PROMOTED component=fleetctl from=built to=dev"* ]]; } \
  && report_pass "gated promotion built->dev" || report_fail "gated promotion built->dev" "$rc :: $o"
n_evt="$(grep -c 'event=promoted component=fleetctl from=built to=dev' "$repo/lifecycle/journal/events.log")"
assert_eq "journal has exactly one promoted line" 1 "$n_evt"
stage_now="$(awk '
  index($0,"  - name: fleetctl")==1 {b=1; next}
  b && /^  - name:/ {exit}
  b && /^[[:space:]]*stage:/ {sub(/^[[:space:]]*stage:[[:space:]]*/,""); print}
' "$repo/ops/state/deployments.yaml")"
assert_eq "state shows stage dev" "dev" "$stage_now"

o="$(f promote fleetctl stage --skip-gates 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"lifecycle/approvals/dev/fleetctl.approved"* ]]; } \
  && report_pass "dev->stage refuses without approval" || report_fail "dev->stage refuses without approval" "$rc :: $o"

o="$(f approve fleetctl stage 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"approvals apply to dev or prod only (got 'stage')"* ]]; } \
  && report_pass "approval stage guard" || report_fail "approval stage guard" "$rc :: $o"

o="$(f approve fleetctl dev ci-tester)"; rc=$?
{ [[ $rc -eq 0 && "$o" == "APPROVED component=fleetctl stage=dev file=lifecycle/approvals/dev/fleetctl.approved" ]]; } \
  && report_pass "approve contract line" || report_fail "approve contract line" "$rc :: $o"
content="$(cat "$repo/lifecycle/approvals/dev/fleetctl.approved")"
assert_contains "approval records actor" "approved_by=ci-tester" "$content"

j_before=$(grep -c '^ts=' "$repo/lifecycle/journal/events.log")
o="$(f promote fleetctl dev 2>&1)"; rc=$?
{ [[ $rc -eq 0 && "$o" == ALREADY\ AT* ]]; } \
  && report_pass "repeat promote = ALREADY AT" || report_fail "repeat promote = ALREADY AT" "$rc :: $o"
j_after=$(grep -c '^ts=' "$repo/lifecycle/journal/events.log")
assert_eq "no-op writes zero journal events" "$j_before" "$j_after"

o="$(f approve fleetctl dev)"; rc=$?
{ [[ $rc -eq 0 && "$o" == ALREADY\ APPROVED* ]]; } \
  && report_pass "repeat approve idempotent" || report_fail "repeat approve idempotent" "$rc :: $o"

o="$(f doctor)"
assert_contains "doctor OK after ceremony" "DOCTOR OK checked_components=2 issues=0" "$o"

finalize
