#!/usr/bin/env bash
# C5b — lifecycle gates integrity.
# The chain must be exactly built->dev->stage->prod, every hop declares its
# units (which must exist on disk) and approvals restricted to {dev,prod},
# and STAGES.md documents the same stages. Static, no mutation.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"

GATES="$FLEET_ROOT/lifecycle/gates.yaml"
STAGES_DOC="$FLEET_ROOT/lifecycle/STAGES.md"

assert_file "gates present" "$GATES"

edges=()
in_gate=false f=""
while IFS= read -r line; do
  if [[ "$line" =~ ^[[:space:]]*-[[:space:]]+from:[[:space:]]*([^[:space:]]+) ]]; then
    [[ -n "$f" ]] && edges+=("$edge")
    f="${BASH_REMATCH[1]}"
    edge="$f|?"
  elif [[ "$line" =~ ^[[:space:]]*to:[[:space:]]*([^[:space:]]+) ]]; then
    edge="$f|${BASH_REMATCH[1]}"
  fi
done <"$GATES"
[[ -n "$f" ]] && edges+=("$edge")

assert_eq "exactly three hops" 3 "${#edges[@]}"
assert_eq "hop1" "built|dev"   "${edges[0]}"
assert_eq "hop2" "dev|stage"   "${edges[1]}"
assert_eq "hop3" "stage|prod"  "${edges[2]}"

# dev->prod shortcut is legal in promote contract; verify it is NOT a gate
# hop itself (gates stay minimal; contract allows skip explicitly).
grep -q "^  - from: dev$" "$GATES" && tos="$(awk '/- from: dev/{flag=1;next} flag&&/^[[:space:]]*to:/{print $2; exit}' "$GATES")"
assert_eq "dev hop targets stage only" "stage" "${tos:-}"

# referenced units exist (post-C6 expectation) OR are known-pending C6c
missing=0 pending_c6c=0
for u in $(sed -n 's/^      - \(C[0-9A-Za-z_]*\)$/\1/p' "$GATES"); do
  if [[ ! -d "$FLEET_ROOT/tests/$u" ]]; then
    if [[ "$u" == "C6c_webapp_contract" ]]; then pending_c6c=$((pending_c6c + 1)); else missing=$((missing + 1)); fi
  fi
done
assert_eq "gate references resolve to real units (C6c allowed pending)" 0 "$((missing))"

# approvals subset check
bad=0
while IFS= read -r a; do
  [[ -z "$a" ]] && continue
  [[ "$a" == dev || "$a" == prod ]] || { report_fail "approval scope '$a'" "only dev|prod allowed"; bad=1; }
done < <(sed -n 's/^      - \(.*\)$/\1/p' <(awk '/needs_approvals:/{a=1;next} a&&!/^      -/{a=0} a' "$GATES"))
[[ $bad -eq 0 ]] && report_pass "approvals scoped to dev|prod"

# doc parity: STAGES.md mentions all four stage names
doc="$(tr -d '\r' <"$STAGES_DOC")"
for s in built dev stage prod; do
  assert_contains "STAGES.md names '$s'" "| $s " "$doc"
done

finalize
