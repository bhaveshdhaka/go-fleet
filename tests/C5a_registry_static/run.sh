#!/usr/bin/env bash
# C5a — registry & environments static analysis.
# Asserts the master registry, environment defs, pipelines and baseline
# state are mutually consistent — pure file inspection, zero mutation.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"

REG="$FLEET_ROOT/ops/PROJECTS.yaml"
ENVY="$FLEET_ROOT/ops/ENVIRONMENTS.yaml"
STATE="$FLEET_ROOT/ops/state/deployments.yaml"
GATES="$FLEET_ROOT/lifecycle/gates.yaml"

assert_file "registry exists" "$REG"
assert_file "environments exist" "$ENVY"
assert_file "state exists" "$STATE"
assert_file "gates exist" "$GATES"

ncomp="$(sed -n 's/^  - name: //p' "$REG" | wc -l | tr -d ' ')"
assert_eq "two components registered" 2 "$ncomp"

# every component entry: required keys non-empty
keys_ok=0
while IFS= read -r c; do
  for k in kind path language pipeline manifests description enabled; do
    v="$(awk -v cc="$c" -v key="$k" '
      index($0,"  - name: "cc)==1 {b=1; next}
      b && /^  - name:/ {exit}
      b && $0 ~ "^    "key":" {sub("^.*"key":[[:space:]]*",""); print; exit}
    ' "$REG")"
    if [[ -n "$v" ]]; then keys_ok=$((keys_ok + 1)); else
      report_fail "component $c has $k" "missing/empty"
    fi
  done
done < <(sed -n 's/^  - name: //p' "$REG")
if [[ $keys_ok -eq $((2 * 7)) ]]; then
  report_pass "all component entries complete (14 keys)"
fi

# baseline state: legal stages only, matching registry names
bad=0
while IFS= read -r c; do
  stage="$(awk -v cc="$c" '
    index($0,"  - name: "cc)==1 {b=1; next}
    b && /^  - name:/ {exit}
    b && /^[[:space:]]*stage:/ {sub(/^[[:space:]]*stage:[[:space:]]*/,""); print}
  ' "$STATE")"
  case "$stage" in built|dev|stage|prod) : ;; *) report_fail "baseline stage legal ($c)" "got '$stage'"; bad=1 ;; esac
done < <(sed -n 's/^  - name: //p' "$REG")
[[ $bad -eq 0 ]] && report_pass "state baselines legal"

# three environments defined incl prod flag
envs_blob="$(tr -d '\r' <"$ENVY")"
for e in dev stage prod; do
  assert_contains "env $e declared" "- name: $e" "$envs_blob"
done
nenv="$(sed -n 's/^  - name: //p' "$ENVY" | wc -l | tr -d ' ')"
assert_eq "exactly three environments" 3 "$nenv"

finalize
