#!/usr/bin/env bash
# C10a — workorder front-matter schema v1 (WO-5).
# wo new emits schema-v1 front-matter; wo list resolves status from
# front-matter and marks legacy files; all live workorders validate.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

repo="$scratch/repo"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo" -xf -

o="$(FLEET_ROOT="$repo" "$F" wo new WO-902 "schema smoke" 2>&1)"
assert_contains "wo new contract line" "WO NEW id=WO-902" "$o"
assert_file "wo file exists" "$repo/workorders/WO-902.md"
head1="$(head -1 "$repo/workorders/WO-902.md")"
assert_eq "front-matter opens with ---" "---" "$head1"
assert_contains "front-matter carries wo id" "wo: WO-902" "$(cat "$repo/workorders/WO-902.md")"
assert_contains "front-matter carries plan link" "plan: PLAN.md" "$(cat "$repo/workorders/WO-902.md")"

o="$(FLEET_ROOT="$repo" "$F" wo list 2>&1)"
assert_contains "new WO listed schema v1 OPEN" "WO id=WO-902 status=OPEN schema=v1" "$o"
n_v1="$(printf '%s\n' "$o" | grep -c 'schema=v1')"
n_legacy="$(printf '%s\n' "$o" | grep -c 'schema=legacy')"
n_total="$(ls "$repo"/workorders/WO-*.md | wc -l | tr -d ' ')"
assert_eq "every workorder carries schema v1" "$n_total" "$n_v1"
assert_eq "no legacy workorders remain" 0 "$n_legacy"

# legacy fallback: a file without front-matter is reported, not crashed
printf '# WO-800 — legacy\n\n> **Status:** OPEN · Owner directive: test\n' > "$repo/workorders/WO-800.md"
o="$(FLEET_ROOT="$repo" "$F" wo list 2>&1)"
assert_contains "legacy file falls back to prose status" "WO id=WO-800 status=OPEN schema=legacy" "$o"

finalize
