#!/usr/bin/env bash
# C16a — site new (WO-15 piece 1), offline. The fresh-install scaffold:
# dry-run is byte-identical across runs (fleet's determinism contract),
# the mutating run creates the lab root (registry skeleton, state,
# 5 embedded templates), appends SITES.yaml, creates the secrets home
# 0700, and REFUSES re-scaffold. The skeleton registry validates via the
# normal ops reader (fake kubectl, rule-7 discipline).

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
export FLEET_SECRETS_HOME="$scratch/secrets-home"
mkdir -p "$FLEET_SECRETS_HOME"

F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"
[[ -x "$F" ]] || { report_fail "binary builds" "ci/build-fleet.sh failed"; finalize; }

mkdir -p "$scratch/repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$scratch/repo" -xf -

# --- dry-run: byte-identical across runs, writes nothing -------------------
FLEET_ROOT="$scratch/repo" "$F" site new drillsite --domain example.test --dry-run > "$scratch/plan1.txt" 2>&1
FLEET_ROOT="$scratch/repo" "$F" site new drillsite --domain example.test --dry-run > "$scratch/plan2.txt" 2>&1
cmp -s "$scratch/plan1.txt" "$scratch/plan2.txt" \
  && report_pass "dry-run byte-equality" \
  || report_fail "dry-run byte-equality" "plans differ"
grep -q "SITE NEW PLAN site=drillsite" "$scratch/plan1.txt" \
  && report_pass "plan contract line" \
  || report_fail "plan contract line" "missing"
[[ -d "$scratch/repo/ops/sites/drillsite" ]] \
  && report_fail "dry-run writes nothing" "site dir exists" \
  || report_pass "dry-run writes nothing"

# --- mutating run -----------------------------------------------------------
o="$(FLEET_ROOT="$scratch/repo" "$F" site new drillsite --domain example.test 2>&1)"
rc=$?
assert_eq "site new rc" "0" "$rc"
assert_contains "site new contract line" "SITE NEW site=drillsite" "$o"
assert_file "registry skeleton" "$scratch/repo/ops/sites/drillsite/config/registry.yaml"
assert_file "state deployed" "$scratch/repo/ops/sites/drillsite/state/deployed.json"
assert_file "template gatus" "$scratch/repo/ops/sites/drillsite/templates/gatus.yaml"
assert_file "template cloudflared" "$scratch/repo/ops/sites/drillsite/templates/cloudflared.yaml"
assert_file "template registry" "$scratch/repo/ops/sites/drillsite/templates/docker-registry.yaml"
[[ -d "$FLEET_SECRETS_HOME/drillsite" ]] \
  && report_pass "secrets home created" \
  || report_fail "secrets home created" "missing"
perm=$(stat -c '%a' "$FLEET_SECRETS_HOME/drillsite")
[[ "$perm" == "700" ]] \
  && report_pass "secrets home mode 0700" \
  || report_fail "secrets home mode 0700" "$perm"
grep -A5 "  - name: drillsite" "$scratch/repo/ops/SITES.yaml" | grep -q "engine: fleet" \
  && report_pass "SITES.yaml entry" \
  || report_fail "SITES.yaml entry" "missing"

# TODO markers are deliberate intent signals
grep -q "TODO_SITE_TUNNEL_CREATE" "$scratch/repo/ops/sites/drillsite/config/registry.yaml" \
  && report_pass "registry skeleton has TODO markers" \
  || report_fail "registry skeleton has TODO markers" "missing"

# --- refusals ---------------------------------------------------------------
o="$(FLEET_ROOT="$scratch/repo" "$F" site new drillsite 2>&1)"
assert_contains "re-new refused" "already registered" "$o"

# --- the skeleton is a parseable site for the ops reader (fake kubectl) ----
mkdir -p "$scratch/bin"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
case "$*" in
  "get nodes -o wide") echo "NAME   STATUS   VERSION" ;;
  "get nodes -o name") echo "node/x" ;;
  "get nodes -o json") echo '{"items":[{"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}' ;;
  *"get pods"*) echo "NAME   READY   STATUS" ;;
  *"jsonpath"*) echo "" ;;
  *) echo "unexpected: $*" >&2; exit 97 ;;
esac
FAKE
chmod +x "$scratch/bin/kubectl"
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops status --site drillsite 2>&1)"
rc=$?
assert_eq "ops status on fresh site rc" "0" "$rc"
assert_contains "fresh site parses (empty services)" "=== services (registry) ===" "$o"

finalize
