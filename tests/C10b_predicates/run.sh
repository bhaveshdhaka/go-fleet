#!/usr/bin/env bash
# C10b — predicates P1-P6 (WO-5) against a real git scratch repo.
# Every predicate is driven to FAIL and back to PASS; non-git contexts
# must SKIP honestly. Nothing outside the scratch copy is touched.

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
# baseline journal: keep only the header + one well-formed event
sed -i '/^# verify/d; /^ts=/d' "$repo/lifecycle/journal/events.log"
printf 'ts=2026-08-27T00:00:00Z event=approved component=fleetctl stage=dev actor=c10\n' >> "$repo/lifecycle/journal/events.log"

check() { FLEET_ROOT="$repo" "$F" check 2>&1; }
st() { grep -E "^CHECK (P1|P2|P3|P4|P5|P6) (PASS|FAIL|SKIP)" | awk '{print $2":"$3}'; }
gitc() { git -C "$repo" -c user.email=c10@fleet -c user.name=c10 "$@"; }

gitc init -q
gitc add -A
gitc commit -qm baseline
# WO-5 is IN_PROGRESS in the copy but its verify lines were scrubbed -> P4 FAIL
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P4 FAIL detail=unjournaled verify: WO-5" \
  && report_pass "P4 fails without journaled verify" || report_fail "P4 fails without journaled verify" "$(printf '%s' "$out" | grep CHECK)"

# P4 back to PASS with a tagged verify line
printf '# verify ts=2026-08-27T00:00:01Z wo=WO-5 piece=1 result=PASS units=1 pass=1 fail=0 skip=0\n' >> "$repo/lifecycle/journal/events.log"
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P4 PASS" \
  && report_pass "P4 passes with journaled verify" || report_fail "P4 passes with journaled verify" "$(printf '%s' "$out" | grep CHECK)"

# P1: no active WO + dirty tree -> FAIL; active WO -> PASS; clean -> PASS
sed -i 's/^status: IN_PROGRESS$/status: EXECUTED/' "$repo/workorders/WO-5.md"
printf '# scratch\n' >> "$repo/README.md"
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P1 FAIL" \
  && report_pass "P1 fails on dirty tree without active WO" || report_fail "P1 fails on dirty tree without active WO" "$(printf '%s' "$out" | grep CHECK)"
sed -i 's/^status: EXECUTED$/status: IN_PROGRESS/' "$repo/workorders/WO-5.md"
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P1 PASS" \
  && report_pass "P1 passes with active WO" || report_fail "P1 passes with active WO" "$(printf '%s' "$out" | grep CHECK)"
gitc checkout -q README.md
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P1 PASS" \
  && report_pass "P1 passes on clean tree" || report_fail "P1 passes on clean tree" "$(printf '%s' "$out" | grep CHECK)"

# P2: break plan link -> FAIL -> PASS
sed -i 's|^plan: PLAN.md$|plan: |' "$repo/workorders/WO-5.md"
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P2 FAIL" \
  && report_pass "P2 fails on missing plan link" || report_fail "P2 fails on missing plan link" "$(printf '%s' "$out" | grep CHECK)"
sed -i 's|^plan: $|plan: PLAN.md|' "$repo/workorders/WO-5.md"

# P3: empty pieces -> FAIL -> PASS
awk 'BEGIN{drop=0} /^pieces:/{drop=1} /^---$/{drop=0} drop==0 || /^pieces:/{print}' \
  "$repo/workorders/WO-5.md" > "$repo/workorders/WO-5.tmp" && mv "$repo/workorders/WO-5.tmp" "$repo/workorders/WO-5.md"
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P3 FAIL" \
  && report_pass "P3 fails on missing decomposition" || report_fail "P3 fails on missing decomposition" "$(printf '%s' "$out" | grep CHECK)"
gitc checkout -q workorders/WO-5.md

# P5: EXECUTED with unintegrated piece -> FAIL -> PASS
sed -i 's/^status: IN_PROGRESS$/status: EXECUTED/; s/^    integrated: true$/    integrated: false/;' "$repo/workorders/WO-5.md"
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P5 FAIL" \
  && report_pass "P5 fails on unintegrated pieces of EXECUTED WO" || report_fail "P5 fails on unintegrated pieces of EXECUTED WO" "$(printf '%s' "$out" | grep CHECK)"
sed -i 's/^    integrated: false$/    integrated: true/' "$repo/workorders/WO-5.md"
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P5 PASS" \
  && report_pass "P5 passes once integrated" || report_fail "P5 passes once integrated" "$(printf '%s' "$out" | grep CHECK)"
sed -i 's/^status: EXECUTED$/status: IN_PROGRESS/' "$repo/workorders/WO-5.md"

# P6: rewriting history (removing a ts= line) -> FAIL -> PASS
gitc add -A && gitc commit -qm wip
grep -v '^ts=' "$repo/lifecycle/journal/events.log" > "$repo/lifecycle/journal/events.tmp" \
  && mv "$repo/lifecycle/journal/events.tmp" "$repo/lifecycle/journal/events.log"
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P6 FAIL" \
  && report_pass "P6 fails on journal history rewrite" || report_fail "P6 fails on journal history rewrite" "$(printf '%s' "$out" | grep CHECK)"
gitc checkout -q lifecycle/journal/events.log
out="$(check)"
printf '%s\n' "$out" | grep -q "^CHECK P6 PASS" \
  && report_pass "P6 passes on append-only journal" || report_fail "P6 passes on append-only journal" "$(printf '%s' "$out" | grep CHECK)"

# summary line + rc discipline
out="$(check)"
assert_contains "check summary present" "CHECK SUMMARY total=6" "$out"
FLEET_ROOT="$repo" "$F" check >/dev/null 2>&1
assert_eq "check rc=0 when clean" 0 "$?"

# non-git context: P1 and P6 SKIP honestly
proj="$scratch/proj"
FLEET_ROOT= "$F" init "$proj" >/dev/null
out="$(FLEET_ROOT="$proj" "$F" check 2>&1)"
printf '%s\n' "$out" | grep -q "^CHECK P1 SKIP detail=not a git repo" \
  && report_pass "P1 skips outside git" || report_fail "P1 skips outside git" "$(printf '%s' "$out" | grep CHECK)"
printf '%s\n' "$out" | grep -q "^CHECK P6 SKIP detail=not a git repo" \
  && report_pass "P6 skips outside git" || report_fail "P6 skips outside git" "$(printf '%s' "$out" | grep CHECK)"

finalize
