#!/usr/bin/env bash
# C14b — ops register + mutations against a FLEET-MANAGED site (WO-9
# piece 2), offline. Migrates a fixture lab with `site init`, then drives
# register/deploy/remove through the BUILT binary with the recording fake
# kubectl. Asserts: canonical registry block, validation refusals, state
# writes land in the SITE dir (not the predecessor), remove restores the
# post-migration byte-baseline, and the predecessor lab fixture is never
# touched by fleet mutations.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"
[[ -x "$F" ]] || { report_fail "binary builds" "ci/build-fleet.sh failed"; finalize; }

mkdir -p "$scratch/repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$scratch/repo" -xf -
mkdir -p "$scratch/fixture"
cp -r "$FLEET_ROOT/internal/fleet/testdata/labfix/." "$scratch/fixture/"

o="$(FLEET_ROOT="$scratch/repo" "$F" site init drillsite --from "$scratch/fixture" 2>&1)"
rc=$?
assert_eq "migration rc" "0" "$rc"
cp "$scratch/repo/ops/sites/drillsite/config/registry.yaml" "$scratch/registry-baseline.yaml"
cp "$scratch/repo/ops/sites/drillsite/state/deployed.json" "$scratch/state-baseline.json"

# recording fake kubectl (same discipline as C13b)
mkdir -p "$scratch/bin" "$scratch/records"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
rest=$(env | sed '/^KUBECONFIG=/d; /^HOME=/d; /^_=/d; /^PWD=/d; /^SHLVL=/d')
if [ -n "$rest" ]; then echo "RULE7 VIOLATION" >&2; exit 99; fi
rec="$(dirname "$0")/../records"
n=$(( $(ls "$rec" 2>/dev/null | grep -c '\.args$') + 1 ))
printf '%s\n' "$*" > "$rec/$n.args"
case "$*" in
  *"rollout status"*|*"rollout undo"*|*"rollout restart"*) : ;;
  *"delete deployment"*|*"delete service"*|*"delete pvc"*|*"delete job"*) : ;;
  *"get rs"*) echo '{"items":[]}' ;;
  apply\ -f\ *gatus.yaml) : ;;
  "apply -f -") echo "configured" ;;
  *) echo "unexpected: $*" >> "$rec/unexpected"; exit 97 ;;
esac
FAKE
chmod +x "$scratch/bin/kubectl"

# --- register -------------------------------------------------------------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops register --site drillsite drillsvc --port 8080 --image busybox:1.36 --secret DRILL_KEY --env DRILL_MSG=hello 2>&1)"
rc=$?
assert_eq "register rc" "0" "$rc"
assert_contains "register contract line" "registered drillsvc" "$o"
grep -A11 '^  drillsvc:' "$scratch/repo/ops/sites/drillsite/config/registry.yaml" > "$scratch/drillsvc-block.txt"
grep -q "namespace: sos-lab" "$scratch/drillsvc-block.txt" && report_pass "block: namespace" || report_fail "block: namespace" "missing"
grep -q "port: 8080" "$scratch/drillsvc-block.txt" && report_pass "block: port" || report_fail "block: port" "missing"
grep -q "enabled: false" "$scratch/drillsvc-block.txt" && report_pass "block: enabled false" || report_fail "block: enabled false" "missing"
grep -q "image: busybox:1.36" "$scratch/drillsvc-block.txt" && report_pass "block: image" || report_fail "block: image" "missing"
grep -q "DRILL_KEY" "$scratch/drillsvc-block.txt" && report_pass "block: secrets" || report_fail "block: secrets" "missing"
grep -q "DRILL_MSG: hello" "$scratch/drillsvc-block.txt" && report_pass "block: env" || report_fail "block: env" "missing"

# refusals
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops register --site drillsite drillsvc --port 8080 --image busybox:1.36 2>&1)"
assert_contains "duplicate register refused" "already registered" "$o"
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops register --site drillsite other --port 99999 --image x 2>&1)"
assert_contains "bad port refused" "1-65535" "$o"
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops register --site drillsite other --port 80 2>&1)"
assert_contains "image-or-repo refused" "needs --image or --repo" "$o"

# fill the declared secret file exactly as the register contract instructs
printf 'DRILL_KEY=drilldummy\n' > "$scratch/fixture/secrets/drillsvc.env"

# --- deploy against the SITE data -----------------------------------------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops deploy --site drillsite drillsvc 2>&1)"
rc=$?
assert_eq "deploy rc" "0" "$rc"
assert_contains "DEPLOYED line" "DEPLOYED drillsvc" "$o"
grep -q '"tag": "1.36"' "$scratch/repo/ops/sites/drillsite/state/deployed.json" \
  && report_pass "state written in SITE dir" \
  || report_fail "state written in SITE dir" "site deployed.json lacks drillsvc"
cmp -s "$scratch/repo/ops/sites/drillsite/state/deployed.json" "$scratch/state-baseline.json" \
  && report_fail "predecessor isolation (site changed)" "site state unchanged?!" \
  || report_pass "site state advanced past baseline"
grep -q "drillsvc" "$scratch/fixture/state/deployed.json" \
  && report_fail "predecessor untouched" "fixture state saw the drill" \
  || report_pass "predecessor lab data untouched"

# --- remove restores byte-baseline ----------------------------------------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops remove --site drillsite drillsvc --unregister 2>&1)"
rc=$?
assert_eq "remove rc" "0" "$rc"
cmp -s "$scratch/repo/ops/sites/drillsite/config/registry.yaml" "$scratch/registry-baseline.yaml" \
  && report_pass "registry back to post-migration baseline" \
  || report_fail "registry back to post-migration baseline" "differs"
cmp -s "$scratch/repo/ops/sites/drillsite/state/deployed.json" "$scratch/state-baseline.json" \
  && report_pass "state back to post-migration baseline" \
  || report_fail "state back to post-migration baseline" "differs"

finalize
