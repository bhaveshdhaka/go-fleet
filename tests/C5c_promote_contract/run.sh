#!/usr/bin/env bash
# C5c — promote contract end-to-end inside an ISOLATED REPO COPY.
# Nothing outside the scratch copy is mutated, ever. Verifies dry-run
# byte-stability, refusal paths, journal append discipline, state rewrite,
# and idempotent repeat (zero-delta).

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
repo="$scratch/fleet"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git -cf - . | tar -C "$repo" -xf -
# isolation: copies inherit live registry state (fleetctl may sit at prod
# after a real ceremony). Reset this copy to the pristine baseline the
# contract expects: every component at 'built', no approvals, empty journal.
sed -i -E 's/^(    stage: ).*/\1built/; s/^(    last_promoted_at: ).*/\1""/' \
  "$repo/ops/state/deployments.yaml"
rm -f "$repo"/lifecycle/approvals/dev/*.approved "$repo"/lifecycle/approvals/prod/*.approved
sed -i '/^ts=/d' "$repo/lifecycle/journal/events.log"

p() { bash "$repo/ci/promote.sh" "$@"; }
hash_tree() { (cd "$1" && find ops lifecycle ci scripts tests -type f -print0 2>/dev/null | sort -z | xargs -0 -r sha256sum | sha256sum); }

d1="$(p fleetctl dev --dry-run 2>&1)"; r1=$?
d2="$(p fleetctl dev --dry-run 2>&1)"
[[ $r1 -eq 0 ]] && report_pass "dry-run dev rc=0" || report_fail "dry-run dev rc=0" "rc=$r1 :: $d1"
assert_eq "dry-run byte-identical" "$d1" "$d2"
h0=$(hash_tree "$repo")

# illegal backwards + non-adjacent + unknown component refused
o="$(p fleetctl prod --dry-run 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"illegal transition"* ]]; } \
  && report_pass "non-adjacent hop refused" || report_fail "non-adjacent hop refused" "$rc :: $o"
o="$(p ghost comp built 2>&1)"; rc=$?
{ [[ $rc -eq 2 || $rc -eq 1 ]]; } \
  && report_pass "unknown component refused" || report_fail "unknown component refused" "$rc :: $o"

h_after_refusals=$(hash_tree "$repo")
assert_eq "refusals mutate nothing" "$h0" "$h_after_refusals"

o="$(bash "$repo/scripts/fleet" approve fleetctl 2>/dev/null)" # wrong arity guard
rc=$?; { [[ $rc -ne 0 ]]; } && report_pass "approve requires stage arg" \
                              || report_fail "approve requires stage arg" "rc=0"

out="$(bash "$FLEET_ROOT/scripts/test.sh" C3c_build_reproducible C4a_deploy_static 2>&1)"
gatefail="$(printf '%s\n' "$out" | sed -n 's/^FLEET SUMMARY.*fail=\([0-9]*\).*$/\1/p' | tail -1)"
assert_eq "gates green before promote runs its own suite" 0 "${gatefail:-x}"

# real promotion WITH gates enforced (promote invokes nested test.sh)
o="$(p fleetctl dev 2>&1)"; rc=$?
{ [[ $rc -eq 0 && "$o" == "PROMOTED component=fleetctl from=built to=dev"* ]]; } \
  && report_pass "real promoted built->dev" || report_fail "real promoted built->dev" "$rc :: $o"

n_evt_promoted="$(grep -c 'event=promoted component=fleetctl from=built to=dev' "$repo/lifecycle/journal/events.log")"
assert_eq "journal has exactly one promoted line" 1 "$n_evt_promoted"
stage_now="$(awk '
  index($0,"  - name: fleetctl")==1 {b=1; next}
  b && /^  - name:/ {exit}
  b && /^[[:space:]]*stage:/ {sub(/^[[:space:]]*stage:[[:space:]]*/,""); print}
' "$repo/ops/state/deployments.yaml")"
assert_eq "state shows stage dev" "dev" "$stage_now"

# now that component sits at dev, dev->stage must refuse on missing approval
o="$(p fleetctl stage --skip-gates 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"lifecycle/approvals/dev/fleetctl.approved"* ]]; } \
  && report_pass "dev->stage refuses without approval file" || report_fail "dev->stage refuses without approval file" "$rc :: $o"

j_before=$(wc -l <"$repo/lifecycle/journal/events.log")
o2="$(p fleetctl dev 2>&1)"; rc=$?
{ [[ $rc -eq 0 && "$o2" == ALREADY\ AT* ]]; } \
  && report_pass "repeat promote = ALREADY AT" || report_fail "repeat promote = ALREADY AT" "$rc :: $o2"
j_after=$(wc -l <"$repo/lifecycle/journal/events.log")
assert_eq "no-op writes zero journal lines" "$j_before" "$j_after"

n_line_fmt_bad=0
while IFS= read -r ln; do
  [[ -z "$ln" || "$ln" == "#"* ]] && continue
  printf '%s\n' "$ln" | grep -Eq '^ts=[^ ]+ event=(approved|promoted|rejected) component=[A-Za-z0-9._-]+' || n_line_fmt_bad=$((n_line_fmt_bad+1))
done <"$repo/lifecycle/journal/events.log"
assert_eq "journal lines well-formed" 0 "$n_line_fmt_bad"

finalize
