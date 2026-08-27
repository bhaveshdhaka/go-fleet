#!/usr/bin/env bash
# C10d — approval-actor policy in .fleet.yaml (WO-5).
# require_human_stages refusals carry the exact fix; allowed actors pass;
# dev stays auto-approvable; a missing .fleet.yaml means unrestricted.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

repo="$scratch/repo"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo" -xf -
sed -i -E 's/^(    stage: ).*/\1built/; s/^(    last_promoted_at: ).*/\1""/' \
  "$repo/ops/state/deployments.yaml"
rm -f "$repo"/lifecycle/approvals/dev/*.approved "$repo"/lifecycle/approvals/prod/*.approved
sed -i '/^ts=/d' "$repo/lifecycle/journal/events.log"

app() { FLEET_ROOT="$repo" "$F" approve "$@"; }

# agent refused on prod, with the exact fix line
o="$(app fleetctl prod 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"actor 'agent' may not approve stage 'prod'"* && "$o" == *"FLEET_ACTOR=<owner|owner-via-agent>"* ]]; } \
  && report_pass "agent refused on prod with fix" || report_fail "agent refused on prod with fix" "$rc :: $o"

# allowed actor passes; file records the human actor
o="$(FLEET_ROOT="$repo" FLEET_ACTOR=owner-via-agent "$F" approve fleetctl prod 2>&1)"; rc=$?
{ [[ $rc -eq 0 && "$o" == APPROVED\ component=fleetctl\ stage=prod* ]]; } \
  && report_pass "allowed actor approves prod" || report_fail "allowed actor approves prod" "$rc :: $o"
assert_contains "approval records human actor" "approved_by=owner-via-agent" \
  "$(cat "$repo/lifecycle/approvals/prod/fleetctl.approved")"

# dev stays auto-approvable (middle hops: FLEET_ACTOR=agent)
o="$(app fleetctl dev 2>&1)"; rc=$?
{ [[ $rc -eq 0 && "$o" == APPROVED\ component=fleetctl\ stage=dev* ]]; } \
  && report_pass "dev auto-approvable as agent" || report_fail "dev auto-approvable as agent" "$rc :: $o"

# missing .fleet.yaml = unrestricted
rm "$repo/.fleet.yaml"
o="$(app fleethub prod 2>&1)"; rc=$?
{ [[ $rc -eq 0 && "$o" == APPROVED\ component=fleethub\ stage=prod* ]]; } \
  && report_pass "missing policy = unrestricted" || report_fail "missing policy = unrestricted" "$rc :: $o"

finalize
