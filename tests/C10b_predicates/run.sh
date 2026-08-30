#!/usr/bin/env bash
# C10b — predicates P1-P7 (WO-5; P7 WO-22) against a real git scratch repo.
# Every predicate is driven to FAIL and back to PASS; the scenario controls
# the copied WO-5's status explicitly so it never depends on live state.
# Nothing outside the scratch copy is touched.

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
# baseline journal: header + one well-formed event; verify comments scrubbed
sed -i '/^# verify/d; /^ts=/d' "$repo/lifecycle/journal/events.log"
printf 'ts=2026-08-27T00:00:00Z event=approved component=fleetctl stage=dev actor=c10\n' >> "$repo/lifecycle/journal/events.log"
# scenario control: neutralize ALL copied workorders to a known state
# (EXECUTED + fully integrated), then force only the ones the scenario drives
for w in "$repo"/workorders/WO-*.md; do
  sed -i 's/^status: OPEN$/status: EXECUTED/; s/^status: IN_PROGRESS$/status: EXECUTED/; s/^    integrated: false$/    integrated: true/' "$w"
done
sed -i 's/^status: EXECUTED$/status: IN_PROGRESS/' "$repo/workorders/WO-5.md"

gitc() { git -C "$repo" -c user.email=c10@fleet -c user.name=c10 "$@"; }
gitc init -q
gitc add -A
gitc commit -qm baseline

check() { FLEET_ROOT="$repo" "$F" check 2>&1; }
getline() { printf '%s\n' "$1" | grep "$2" | head -1; }

# P4: IN_PROGRESS + no wo=WO-5 verify line -> FAIL
out="$(check)"
line="$(getline "$out" '^CHECK P4 ')"
{ [[ "$line" == "CHECK P4 FAIL detail=unjournaled verify: WO-5"* ]]; } \
  && report_pass "P4 fails without journaled verify" || report_fail "P4 fails without journaled verify" "$line"

# P4 back to PASS with a tagged verify line
printf '# verify ts=2026-08-27T00:00:01Z wo=WO-5 piece=1 result=PASS units=1 pass=1 fail=0 skip=0\n' >> "$repo/lifecycle/journal/events.log"
line="$(getline "$(check)" '^CHECK P4 ')"
{ [[ "$line" == "CHECK P4 PASS"* ]]; } \
  && report_pass "P4 passes with journaled verify" || report_fail "P4 passes with journaled verify" "$line"

# P1: EXECUTED WO-5 + dirty tree -> FAIL; IN_PROGRESS -> PASS; clean -> PASS
sed -i 's/^status: IN_PROGRESS$/status: EXECUTED/' "$repo/workorders/WO-5.md"
printf '# scratch\n' >> "$repo/README.md"
line="$(getline "$(check)" '^CHECK P1 ')"
{ [[ "$line" == "CHECK P1 FAIL"* ]]; } \
  && report_pass "P1 fails on dirty tree without active WO" || report_fail "P1 fails on dirty tree without active WO" "$line"
sed -i 's/^status: EXECUTED$/status: IN_PROGRESS/' "$repo/workorders/WO-5.md"
line="$(getline "$(check)" '^CHECK P1 ')"
{ [[ "$line" == "CHECK P1 PASS"* ]]; } \
  && report_pass "P1 passes with active WO" || report_fail "P1 passes with active WO" "$line"
gitc checkout -q README.md
line="$(getline "$(check)" '^CHECK P1 ')"
{ [[ "$line" == "CHECK P1 PASS"* ]]; } \
  && report_pass "P1 passes on clean tree" || report_fail "P1 passes on clean tree" "$line"

# P2: break plan link -> FAIL -> PASS
sed -i 's|^plan: PLAN.md$|plan: |' "$repo/workorders/WO-5.md"
line="$(getline "$(check)" '^CHECK P2 ')"
{ [[ "$line" == "CHECK P2 FAIL"* ]]; } \
  && report_pass "P2 fails on missing plan link" || report_fail "P2 fails on missing plan link" "$line"
sed -i 's|^plan: $|plan: PLAN.md|' "$repo/workorders/WO-5.md"
line="$(getline "$(check)" '^CHECK P2 ')"
{ [[ "$line" == "CHECK P2 PASS"* ]]; } \
  && report_pass "P2 passes with plan link" || report_fail "P2 passes with plan link" "$line"

# P3: strip the pieces list -> FAIL -> PASS
awk 'BEGIN{drop=0} /^pieces:/{drop=1; print; next} /^---$/{drop=0} drop==0{print}' \
  "$repo/workorders/WO-5.md" > "$repo/workorders/WO-5.tmp" && mv "$repo/workorders/WO-5.tmp" "$repo/workorders/WO-5.md"
line="$(getline "$(check)" '^CHECK P3 ')"
{ [[ "$line" == "CHECK P3 FAIL"* ]]; } \
  && report_pass "P3 fails on missing decomposition" || report_fail "P3 fails on missing decomposition" "$line"
gitc checkout -q workorders/WO-5.md
line="$(getline "$(check)" '^CHECK P3 ')"
{ [[ "$line" == "CHECK P3 PASS"* ]]; } \
  && report_pass "P3 passes with decomposition" || report_fail "P3 passes with decomposition" "$line"

# P5: EXECUTED + unintegrated piece -> FAIL; all integrated -> PASS
sed -i 's/^status: IN_PROGRESS$/status: EXECUTED/; s/^    integrated: true$/    integrated: false/' "$repo/workorders/WO-5.md"
line="$(getline "$(check)" '^CHECK P5 ')"
{ [[ "$line" == "CHECK P5 FAIL"* ]]; } \
  && report_pass "P5 fails on unintegrated pieces of EXECUTED WO" || report_fail "P5 fails on unintegrated pieces of EXECUTED WO" "$line"
sed -i 's/^    integrated: false$/    integrated: true/' "$repo/workorders/WO-5.md"
line="$(getline "$(check)" '^CHECK P5 ')"
{ [[ "$line" == "CHECK P5 PASS"* ]]; } \
  && report_pass "P5 passes once integrated" || report_fail "P5 passes once integrated" "$line"
sed -i 's/^status: EXECUTED$/status: IN_PROGRESS/' "$repo/workorders/WO-5.md"

# P6: rewriting history (removing a ts= line) -> FAIL -> PASS
gitc add -A && gitc commit -qm wip
grep -v '^ts=' "$repo/lifecycle/journal/events.log" > "$repo/lifecycle/journal/events.tmp" \
  && mv "$repo/lifecycle/journal/events.tmp" "$repo/lifecycle/journal/events.log"
line="$(getline "$(check)" '^CHECK P6 ')"
{ [[ "$line" == "CHECK P6 FAIL"* ]]; } \
  && report_pass "P6 fails on journal history rewrite" || report_fail "P6 fails on journal history rewrite" "$line"
gitc checkout -q lifecycle/journal/events.log
line="$(getline "$(check)" '^CHECK P6 ')"
{ [[ "$line" == "CHECK P6 PASS"* ]]; } \
  && report_pass "P6 passes on append-only journal" || report_fail "P6 passes on append-only journal" "$line"

# P7: active WO without a journey ref -> FAIL; journeys_exempt -> PASS
# (WO-5 is IN_PROGRESS here with a journaled verify but verify fields
# name no journey unit — exactly the drift P7 reports; rule 9)
line="$(getline "$(check)" '^CHECK P7 ')"
{ [[ "$line" == "CHECK P7 FAIL"* ]]; } \
  && report_pass "P7 fails on active WO without journey coverage" || report_fail "P7 fails on active WO without journey coverage" "$line"
sed -i 's/^plan: PLAN.md$/plan: PLAN.md\njourneys_exempt: true/' "$repo/workorders/WO-5.md"
line="$(getline "$(check)" '^CHECK P7 ')"
{ [[ "$line" == "CHECK P7 PASS"* ]]; } \
  && report_pass "P7 passes with journeys_exempt" || report_fail "P7 passes with journeys_exempt" "$line"

# summary line + rc discipline
out="$(check)"
assert_contains "check summary present" "CHECK SUMMARY total=7" "$out"
FLEET_ROOT="$repo" "$F" check >/dev/null 2>&1
assert_eq "check rc=0 when clean" 0 "$?"

# non-git context: P1 and P6 SKIP honestly
proj="$scratch/proj"
FLEET_ROOT= "$F" init "$proj" >/dev/null
out="$(FLEET_ROOT="$proj" "$F" check 2>&1)"
line="$(getline "$out" '^CHECK P1 ')"
{ [[ "$line" == "CHECK P1 SKIP detail=not a git repo" ]]; } \
  && report_pass "P1 skips outside git" || report_fail "P1 skips outside git" "$line"
line="$(getline "$out" '^CHECK P6 ')"
{ [[ "$line" == "CHECK P6 SKIP detail=not a git repo" ]]; } \
  && report_pass "P6 skips outside git" || report_fail "P6 skips outside git" "$line"

finalize
