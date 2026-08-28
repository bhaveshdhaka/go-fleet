#!/usr/bin/env bash
# C13b — ops mutation verbs, offline drill (WO-8 piece 2).
# Drives the BUILT fleet binary through deploy/rollback/remove against a
# recording fake kubectl. The fake enforces the rule-7 child-env contract
# (ONLY KUBECONFIG + HOME; bash-injected _/PWD/SHLVL tolerated) and
# records every call: args + stdin, one numbered pair per call.
# Asserts: lab-identical machine lines, apply order (secret -> deployment
# -> service -> 6 monitor docs -> gatus template -> rollouts), state-file
# writes, registry enabled flip, remove state cleanup (the journaled
# fleet extension), refusal shapes. Cloudflare write paths are covered by
# the Go httptest suite; the LIVE dual-run is piece 3.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# fresh binary from the live source
F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"
[[ -x "$F" ]] || { report_fail "binary builds" "ci/build-fleet.sh failed"; finalize; }

# scratch repo (SITES.yaml rewritten to point at the fixture lab)
mkdir -p "$scratch/repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$scratch/repo" -xf -
# lab fixture (routed services in fixture: alpha only -> 1 endpoint)
mkdir -p "$scratch/lab"
cp -r "$FLEET_ROOT/internal/fleet/testdata/labfix/." "$scratch/lab/"
printf 'BETA_KEY=fixturedummy\n' > "$scratch/lab/secrets/beta.env"
: > "$scratch/lab/drill.kubeconfig"

cat > "$scratch/repo/ops/SITES.yaml" <<EOF
sites_version: 1
sites:
  - name: drill
    engine: sos-lab
    lab_root: $scratch/lab
    namespace: sos-lab
    access: kubeconfig:drill.kubeconfig
    description: C13b offline drill fixture
EOF

# recording fake kubectl (self-relative records dir; env discipline)
mkdir -p "$scratch/bin" "$scratch/records"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
rest=$(env | sed '/^KUBECONFIG=/d; /^HOME=/d; /^_=/d; /^PWD=/d; /^SHLVL=/d')
if [ -n "$rest" ]; then
  echo "RULE7 VIOLATION: kubectl child env leaked ambient variables:" >&2
  printf '%s\n' "$rest" | sed 's/=.*/=<redacted>/' >&2
  exit 99
fi
rec="$(dirname "$0")/../records"
n=$(( $(ls "$rec" 2>/dev/null | grep -c '\.args$') + 1 ))
printf '%s\n' "$*" > "$rec/$n.args"
if [ "$1" = "apply" ] && [ "$2" = "-f" ] && [ "$3" = "-" ]; then
  cat > "$rec/$n.stdin"
fi
case "$*" in
  *"create secret generic"*) printf 'apiVersion: v1\nkind: Secret\nmetadata:\n  name: beta-env\n' ;;
  "apply -f -") echo "configured" ;;
  apply\ -f\ *gatus.yaml) : ;;
  *"rollout status"*|*"rollout undo"*|*"rollout restart"*) : ;;
  *"delete deployment"*|*"delete service"*|*"delete pvc"*|*"delete job"*) : ;;
  *"get rs"*) cat "$rec/rs.json" ;;
  *"get pods"*) echo '{"items":[{"metadata":{"name":"build-pod-0"}}]}' ;;
  "logs -f pod") echo "build-log-line" ;;
  *"wait --for=condition=complete"*) : ;;
  *) echo "unexpected: $*" >> "$rec/unexpected"; exit 97 ;;
esac
FAKE
chmod +x "$scratch/bin/kubectl"
cat > "$scratch/records/rs.json" <<'EOF'
{"items":[
 {"metadata":{"annotations":{"deployment.kubernetes.io/revision":"1"}},
  "spec":{"template":{"spec":{"containers":[{"image":"reg.local:5000/beta:bt1"}]}}}},
 {"metadata":{"annotations":{"deployment.kubernetes.io/revision":"2"}},
  "spec":{"template":{"spec":{"containers":[{"image":"reg.local:5000/beta:bt2"}]}}}}]}
EOF

# --- deploy beta (repo service, build bt1, no host -> no CF calls) -------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops deploy beta 2>&1)"
rc=$?
assert_eq "deploy rc" "0" "$rc"
assert_contains "deploy: per-service deployed line" "deployed beta tag=bt1" "$o"
assert_contains "deploy: done marker" "-- beta done" "$o"
assert_contains "deploy: monitor line (routed=alpha only)" \
  "MONITOR OK: gatus + dashboard synced (1 endpoints)" "$o"
assert_contains "deploy: DEPLOYED contract" "DEPLOYED beta" "$o"
[[ -f "$scratch/records/unexpected" ]] \
  && report_fail "no unexpected kubectl calls" "$(cat "$scratch/records/unexpected")" \
  || report_pass "no unexpected kubectl calls"

# call sequence 1..15: secret create, secret apply, dep, svc, rollout,
# 6 monitor applies, gatus template, 3 rollouts
seq_expect=(
  *"create secret generic beta-env"*
  "apply -f -" "apply -f -" "apply -f -"
  *"rollout status deployment/beta"*
  "apply -f -" "apply -f -" "apply -f -" "apply -f -" "apply -f -" "apply -f -"
  "apply -f $scratch/lab/templates/gatus.yaml"
  *"rollout status deployment/sos-dashboard"*
  *"rollout restart deployment/gatus"*
  *"rollout status deployment/gatus"*
)
seq_ok=yes
for i in "${!seq_expect[@]}"; do
  n=$((i + 1))
  # shellcheck disable=SC2053
  if ! [[ "$(cat "$scratch/records/$n.args")" == ${seq_expect[$i]} ]]; then
    report_fail "call $n sequence" "want '${seq_expect[$i]}' got '$(cat "$scratch/records/$n.args")'"
    seq_ok=no
  fi
done
[[ $seq_ok == yes ]] && report_pass "kubectl call sequence matches lab order"

# apply kinds in order: Secret (from create stdout), dep, svc, then monitor
kinds=""
for n in 2 3 4 6 7 8 9 10 11; do
  f="$scratch/records/$n.stdin"
  k="$(grep -m1 '^kind:' "$f" | awk '{print $2}')"
  [[ -z "$k" ]] && k="$(grep -o '"kind":"[A-Za-z]*"' "$f" | head -1 | sed 's/.*:"//; s/"//')"
  kinds="$kinds $k"
done
assert_eq "applied doc kinds in lab order" \
  " Secret Deployment Service ConfigMap ConfigMap ConfigMap ConfigMap Deployment Service" "$kinds"

# state written in labctl byte format; registry flipped
grep -q '"tag": "bt1"' "$scratch/lab/state/deployed.json" \
  && report_pass "deployed.json records beta bt1" \
  || report_fail "deployed.json records beta bt1" "missing"
grep -q '"image": "docker-registry.sos-lab.svc.cluster.local:5000/beta:bt1"' "$scratch/lab/state/deployed.json" \
  && report_pass "deployed.json records image" \
  || report_fail "deployed.json records image" "missing"
sed -n '/^  beta:/,/^  gamma:/p' "$scratch/lab/config/registry.yaml" | grep -q 'enabled: true' \
  && report_pass "registry enabled flipped" \
  || report_fail "registry enabled flipped" "beta still disabled"
grep -q 'secretRef' "$scratch/records/3.stdin" \
  && report_pass "envFrom secretRef (secrets file present)" \
  || report_fail "envFrom secretRef" "missing in deployment doc"

# --- rollback (canned rs: prev = bt1) ------------------------------------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops rollback beta 2>&1)"
rc=$?
assert_eq "rollback rc" "0" "$rc"
assert_contains "rollback machine line" \
  "rolled back beta; state now records tag=bt1 image=reg.local:5000/beta:bt1 (rolled_back=true)" "$o"
grep -q '"rolled_back": true' "$scratch/lab/state/deployed.json" \
  && report_pass "rollback state record" \
  || report_fail "rollback state record" "missing rolled_back flag"

# --- refusals -------------------------------------------------------------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops deploy nonexistent 2>&1)"
rc=$?
assert_eq "deploy unknown rc=1" "1" "$rc"
assert_contains "deploy unknown ERROR shape" "ERROR: service 'nonexistent' is not registered" "$o"
# make beta REQUIRE a secret key so the two-phase check has teeth
before=$(ls "$scratch/records" | grep -c '\.args$')
mv "$scratch/lab/secrets/beta.env" "$scratch/beta.env.bak"
sed -i '/^  beta:/,/^  gamma:/ s/    secrets: \[\]/    secrets:\n    - BETA_KEY/' "$scratch/lab/config/registry.yaml"
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops deploy beta 2>&1)"
rc=$?
assert_eq "deploy without secrets file rc=1" "1" "$rc"
assert_contains "two-phase secret check refuses BEFORE any kubectl" "missing file" "$o"
assert_contains "secret check names the missing key" "BETA_KEY" "$o"
after=$(ls "$scratch/records" | grep -c '\.args$')
assert_eq "no kubectl calls on secret refusal" "$before" "$after"
mv "$scratch/beta.env.bak" "$scratch/lab/secrets/beta.env"
sed -i '/^  beta:/,/^  gamma:/ { /^    - BETA_KEY$/d; s/^    secrets:$/    secrets: []/; }' "$scratch/lab/config/registry.yaml"
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops verify beta 2>&1)"
assert_contains "verify hostless refusal" "service 'beta' has no public host" "$o"

# --- remove (fleet extension: state cleanup) ------------------------------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops remove beta --delete-data --unregister 2>&1)"
rc=$?
assert_eq "remove rc" "0" "$rc"
assert_contains "remove contract line" "removed beta from cluster and registry" "$o"
assert_contains "remove runs monitor" "MONITOR OK: gatus + dashboard synced (1 endpoints)" "$o"
grep -q '"beta"' "$scratch/lab/state/deployed.json" \
  && report_fail "remove cleans deployed.json" "beta still present" \
  || report_pass "remove cleans deployed.json"
grep -q '"beta"' "$scratch/lab/state/builds.json" \
  && report_fail "remove cleans builds.json" "beta still present" \
  || report_pass "remove cleans builds.json"
grep -q '^  beta:' "$scratch/lab/config/registry.yaml" \
  && report_fail "remove unregisters block" "beta block still present" \
  || report_pass "remove unregisters block"
grep -q "delete pvc beta-data" "$scratch/records"/*.args \
  && report_pass "delete-data removes PVC" \
  || report_fail "delete-data removes PVC" "no pvc delete recorded"

finalize
