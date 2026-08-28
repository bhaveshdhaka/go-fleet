#!/usr/bin/env bash
# C17c — --json surface + exit codes (WO-17), hermetic. Every read verb
# emits parseable JSON ADDITIVELY (text machine contracts byte-identical
# without the flag), and the exit codes are as documented: 0 ok, 1 fail,
# 2 usage/refused. Shape asserts run through tests/lib/jsonq (stdlib Go).

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"
[[ -x "$F" ]] || { report_fail "binary builds" "ci/build-fleet.sh failed"; finalize; }
jsonq_build "$scratch/jsonq" || { report_fail "jsonq builds" "go build failed"; finalize; }
J="$scratch/jsonq"

mkdir -p "$scratch/repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$scratch/repo" -xf -

# status --json: parses; components list present; text mode unchanged
FLEET_ROOT="$scratch/repo" "$F" status --json > "$scratch/status.json" 2>&1
"$J" "$scratch/status.json" valid 2>/dev/null \
  && report_pass "status --json parses" || report_fail "status --json parses" "$(head -c 120 "$scratch/status.json")"
[[ "$("$J" "$scratch/status.json" type .components 2>/dev/null)" == "list" ]] \
  && report_pass "status --json components" || report_fail "status --json components" "missing"
o="$(FLEET_ROOT="$scratch/repo" "$F" status 2>&1)"
assert_contains "status text contract kept" "STATUS SUMMARY components=" "$o"

# doctor --json: ok:true on a healthy tree (issues list may be null)
FLEET_ROOT="$scratch/repo" "$F" doctor --json > "$scratch/doctor.json" 2>&1
[[ "$("$J" "$scratch/doctor.json" str .ok 2>/dev/null)" == "true" ]] \
  && report_pass "doctor --json ok:true" || report_fail "doctor --json ok:true" "$(cat "$scratch/doctor.json")"

# next --json: parses; action key present (no git -> P1 skips -> action exists)
FLEET_ROOT="$scratch/repo" "$F" next --json > "$scratch/next.json" 2>&1
{ "$J" "$scratch/next.json" has . action 2>/dev/null && "$J" "$scratch/next.json" has . reason 2>/dev/null; } \
  && report_pass "next --json shape" || report_fail "next --json shape" "$(cat "$scratch/next.json")"

# check --json: predicates array + counts, and rc=1 when a predicate fails
FLEET_ROOT="$scratch/repo" "$F" check --json > "$scratch/check.json" 2>&1
{ [[ "$("$J" "$scratch/check.json" type .predicates 2>/dev/null)" == "list" ]] && "$J" "$scratch/check.json" has . pass 2>/dev/null && "$J" "$scratch/check.json" has . fail 2>/dev/null; } \
  && report_pass "check --json shape" || report_fail "check --json shape" "$(head -c 120 "$scratch/check.json")"

# site list --json
FLEET_ROOT="$scratch/repo" "$F" site list --json > "$scratch/sites.json" 2>&1
[[ "$("$J" "$scratch/sites.json" type .sites 2>/dev/null)" == "list" ]] \
  && report_pass "site list --json shape" || report_fail "site list --json shape" "bad"

# ops status --json (fake kubectl, rule-7 discipline)
mkdir -p "$scratch/lab" "$scratch/bin"
cp -r "$FLEET_ROOT/internal/fleet/testdata/labfix/." "$scratch/lab/"
: > "$scratch/drill.kubeconfig"
cat > "$scratch/repo/ops/SITES.yaml" <<EOF
sites_version: 1
sites:
  - name: drill
    engine: sos-lab
    lab_root: $scratch/lab
    namespace: sos-lab
    access: kubeconfig:$scratch/drill.kubeconfig
    description: C17c
EOF
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
rest=$(env | sed '/^KUBECONFIG=/d; /^HOME=/d; /^_=/d; /^PWD=/d; /^SHLVL=/d')
if [ -n "$rest" ]; then echo "RULE7 VIOLATION" >&2; exit 99; fi
case "$*" in
  *"get nodes"*) echo "node/x" ;;
  *"get pods"*) echo "NAME READY STATUS" ;;
  *"jsonpath"*) echo "" ;;
  *) echo "unexpected: $*" >&2; exit 97 ;;
esac
FAKE
chmod +x "$scratch/bin/kubectl"
FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops status --site drill --json > "$scratch/opsstatus.json" 2>&1
{ [[ "$("$J" "$scratch/opsstatus.json" str .site 2>/dev/null)" == "drill" ]] && [[ "$("$J" "$scratch/opsstatus.json" type .services 2>/dev/null)" == "list" ]]; } \
  && report_pass "ops status --json shape" || report_fail "ops status --json shape" "$(head -c 120 "$scratch/opsstatus.json")"

# exit codes: 0 ok / 1 fail (failing predicate) / 2 usage refused
FLEET_ROOT="$scratch/repo" "$F" check >/dev/null 2>&1; rc0=$?
# force P4 (unjournaled verify on an IN_PROGRESS workorder)
cat > "$scratch/repo/workorders/WO-99.md" <<'EOF'
---
wo: WO-99
title: C17c exit-code fixture
status: IN_PROGRESS
plan: PLAN.md
pieces:
  - id: 1
    title: fixture piece
    verify: none
    integrated: false
---
EOF
FLEET_ROOT="$scratch/repo" "$F" check >/dev/null 2>&1; rc1=$?
FLEET_ROOT="$scratch/repo" "$F" ops bogus-sub >/dev/null 2>&1; rc2=$?
assert_eq "exit 0 on healthy check" "0" "$rc0"
assert_eq "exit 1 on failing check" "1" "$rc1"
assert_eq "exit 2 on usage refusal" "2" "$rc2"

finalize
