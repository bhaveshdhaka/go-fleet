#!/usr/bin/env bash
# C10c — next guidance engine (WO-5): predicates drive the suggestion with
# NEXT predicate=P<N>, then doctor precheck, then the first lifecycle hop.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

# git-backed copy, pristine baseline, WO-5 scrubbed to a verifiable state
repo="$scratch/repo"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo" -xf -
sed -i -E 's/^(    stage: ).*/\1built/; s/^(    last_promoted_at: ).*/\1""/' \
  "$repo/ops/state/deployments.yaml"
rm -f "$repo"/lifecycle/approvals/dev/*.approved "$repo"/lifecycle/approvals/prod/*.approved
sed -i '/^# verify/d; /^ts=/d' "$repo/lifecycle/journal/events.log"
printf 'ts=2026-08-27T00:00:00Z event=approved component=fleetctl stage=dev actor=c10\n' >> "$repo/lifecycle/journal/events.log"
printf '# verify ts=2026-08-27T00:00:01Z wo=WO-5 piece=1 result=PASS units=1 pass=1 fail=0 skip=0\n' >> "$repo/lifecycle/journal/events.log"
sed -i 's/^status: IN_PROGRESS$/status: EXECUTED/; s/^    integrated: false$/    integrated: true/' "$repo/workorders/WO-5.md"
gitc() { git -C "$repo" -c user.email=c10@fleet -c user.name=c10 "$@"; }
gitc init -q && gitc add -A && gitc commit -qm baseline

next() { FLEET_ROOT="$repo" "$F" next 2>&1; }

# 1. P1 drives guidance when the tree is dirty without an active WO
printf '# drift\n' >> "$repo/README.md"
o="$(next)"
assert_contains "P1 drives next on dirty tree" "NEXT predicate=P1" "$o"
assert_contains "P1 fix is the wo new command" "NEXT action=./scripts/fleet wo new" "$o"
gitc checkout -q README.md

# 2. lifecycle guidance on a clean, predicate-clean repo: fleetctl at built
o="$(next)"
assert_contains "lifecycle suggestion follows clean predicates" \
  "NEXT action=./scripts/fleet promote fleetctl dev" "$o"

# 3. fleetctl promoted to dev + dev approval present -> next hop is stage
sed -i -E 's/^(    stage: ).*/\1dev/' "$repo/ops/state/deployments.yaml"
gitc add -A && gitc commit -qm fleetctl-dev
mkdir -p "$repo/lifecycle/approvals/dev"
printf 'approved_by=c10\nts=2026-08-27T00:00:02Z\n' > "$repo/lifecycle/approvals/dev/fleetctl.approved"
gitc add -A && gitc commit -qm approval
o="$(next)"
assert_contains "next walks the approved hop" \
  "NEXT action=./scripts/fleet promote fleetctl stage" "$o"

# 4. predicates outrank lifecycle: break P4 again
sed -i '/^# verify/d' "$repo/lifecycle/journal/events.log"
sed -i 's/^status: EXECUTED$/status: IN_PROGRESS/' "$repo/workorders/WO-5.md"
o="$(next)"
assert_contains "P4 outranks lifecycle" "NEXT predicate=P4" "$o"

finalize
