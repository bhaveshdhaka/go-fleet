#!/usr/bin/env bash
# C19a — secrets audit gate (WO-19). The repo is PUBLIC: tree + full git
# history must be free of value-shaped secrets. A planted finding must
# fail; the clean repo must pass.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# 1. clean repo passes
o="$(bash "$FLEET_ROOT/ci/audit-secrets.sh" 2>&1)"
rc=$?
assert_eq "audit rc=0 on clean repo" "0" "$rc"
assert_contains "audit clean contract" "SECRETS AUDIT OK" "$o"

# 2. a planted real-shaped value FAILS (in an excluded tests/ dir? no —
#    tests/ is excluded; plant in ci/ fixture then remove)
printf 'CF_API_TOKEN=AbC123dEfGhIjKlMnOpQrStUvWxYz0123456789\n' > "$FLEET_ROOT/ci/.audit-fixture"
o="$(bash "$FLEET_ROOT/ci/audit-secrets.sh" 2>&1)"
rc=$?
rm -f "$FLEET_ROOT/ci/.audit-fixture"
assert_eq "audit rc=1 on planted value" "1" "$rc"
assert_contains "audit names the finding" "CF_API_TOKEN" "$o"
[[ ! -f "$FLEET_ROOT/ci/.audit-fixture" ]] \
  && report_pass "fixture cleaned up" \
  || report_fail "fixture cleaned up" "left behind"

finalize
