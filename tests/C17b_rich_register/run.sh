#!/usr/bin/env bash
# C17b — rich ops register (WO-16 piece 1), offline. Registers a service
# with the FULL runtime surface (probePath, runAsUser, serviceAccount,
# args, mem/cpu request:limit, storage+mount, mount-sub, mount-host) and
# deploys it against a recording fake kubectl, asserting the canonical
# registry block AND the rendered pod spec (securityContext, resources,
# volumeMounts, probe path, args).

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
export FLEET_SECRETS_HOME="$scratch/secrets-home"
mkdir -p "$FLEET_SECRETS_HOME"

F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"
[[ -x "$F" ]] || { report_fail "binary builds" "ci/build-fleet.sh failed"; finalize; }

mkdir -p "$scratch/repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$scratch/repo" -xf -

# site from labfix (alpha only); the rich service is fresh-registered
mkdir -p "$scratch/lab"
cp -r "$FLEET_ROOT/internal/fleet/testdata/labfix/." "$scratch/lab/"
: > "$scratch/drill.kubeconfig"
mkdir -p "$FLEET_SECRETS_HOME/drill"
printf 'RICH_KEY=dummy\n' > "$FLEET_SECRETS_HOME/drill/rich.env"
printf 'DASHBOARD_SLUG=richdash\n' > "$FLEET_SECRETS_HOME/drill/dashboard.env"

cat > "$scratch/repo/ops/SITES.yaml" <<EOF
sites_version: 1
sites:
  - name: drill
    engine: sos-lab
    lab_root: $scratch/lab
    namespace: sos-lab
    access: kubeconfig:$scratch/drill.kubeconfig
    description: C17b rich register fixture
EOF

mkdir -p "$scratch/bin" "$scratch/records"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
rec="$(dirname "$0")/../records"
n=$(( $(ls "$rec" 2>/dev/null | grep -c '\.args$') + 1 ))
printf '%s\n' "$*" > "$rec/$n.args"
if [ "$1" = "apply" ] && [ "$2" = "-f" ] && [ "$3" = "-" ]; then cat > "$rec/$n.stdin"; fi
case "$*" in
  *"create secret generic"*) printf 'apiVersion: v1\nkind: Secret\n' ;;
  "apply -f -") echo "configured" ;;
  apply\ -f\ *) : ;;
  *"rollout status"*|*"rollout undo"*|*"rollout restart"*) : ;;
  *"delete deployment"*|*"delete service"*|*"delete pvc"*|*"delete job"*) : ;;
  *"get rs"*) echo '{"items":[]}' ;;
  *"get pods"*) echo '{"items":[{"metadata":{"name":"p-0"}}]}' ;;
  *"logs -f"*) echo build-log ;;
  *"wait --for=condition=complete"*) : ;;
  *"get nodes"*) echo node ;;
  *"jsonpath"*) echo "" ;;
  *) echo "unexpected: $*" >> "$rec/unexpected"; exit 97 ;;
esac
FAKE
chmod +x "$scratch/bin/kubectl"

o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops register --site drill rich \
  --port 9000 --image ghcr.io/x/rich:1 \
  --probe-path /health --run-as-user 65532 --service-account rich-sa \
  --args serve --args --foreground \
  --mem 256Mi:2Gi --cpu 100m:1 \
  --storage 5Gi:/data \
  --mount-sub config:/etc/rich \
  --mount-host /workspace:/workspace 2>&1)"
rc=$?
assert_eq "rich register rc" "0" "$rc"
assert_contains "rich register ok" "registered rich" "$o"

# canonical registry block, mini-yaml + pyyaml both parse (revalidate)
reg="$scratch/lab/config/registry.yaml"
grep -q "    probePath: /health" "$reg" && report_pass "probePath persisted" || report_fail "probePath persisted" "missing"
grep -q "    runAsUser: 65532" "$reg" && report_pass "runAsUser persisted" || report_fail "runAsUser persisted" "missing"
grep -q "    serviceAccount: rich-sa" "$reg" && report_pass "serviceAccount persisted" || report_fail "serviceAccount persisted" "missing"
grep -q "      size: 5Gi" "$reg" && report_pass "storage size persisted" || report_fail "storage size persisted" "missing"
grep -q "      mount: /data" "$reg" && report_pass "storage mount persisted" || report_fail "storage mount persisted" "missing"
grep -q "    - sub: config" "$reg" && report_pass "mount-sub persisted" || report_fail "mount-sub persisted" "missing"
grep -q "    - host: /workspace" "$reg" && report_pass "mount-host persisted" || report_fail "mount-host persisted" "missing"
grep -q "        memory: 2Gi" "$reg" && report_pass "mem limit persisted" || report_fail "mem limit persisted" "missing"
grep -q "        cpu: 100m" "$reg" && report_pass "cpu request persisted" || report_fail "cpu request persisted" "missing"

# deploy renders the pod spec from the rich fields
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops deploy --site drill rich 2>&1)"
rc=$?
assert_eq "rich deploy rc" "0" "$rc"
assert_contains "rich DEPLOYED" "DEPLOYED rich" "$o"

cat "$scratch/records"/*.stdin > "$scratch/all-docs.json" 2>/dev/null
grep -q '"runAsUser":65532' "$scratch/all-docs.json" && report_pass "pod spec: runAsUser" || report_fail "pod spec: runAsUser" "missing"
grep -q '"memory":"2Gi"' "$scratch/all-docs.json" && report_pass "pod spec: mem limit" || report_fail "pod spec: mem limit" "missing"
grep -q '/etc/rich' "$scratch/all-docs.json" && report_pass "pod spec: sub mount" || report_fail "pod spec: sub mount" "missing"
grep -q 'PersistentVolumeClaim' "$scratch/all-docs.json" && report_pass "pod spec: PVC rendered" || report_fail "pod spec: PVC rendered" "missing"
grep -q '"rich-sa"' "$scratch/all-docs.json" && report_pass "pod spec: serviceAccount" || report_fail "pod spec: serviceAccount" "missing"
grep -q '"--foreground"' "$scratch/all-docs.json" && report_pass "pod spec: args" || report_fail "pod spec: args" "missing"
grep -q '"/health"' "$scratch/all-docs.json" && report_pass "pod spec: probe path" || report_fail "pod spec: probe path" "missing"
grep -q '"/workspace"' "$scratch/all-docs.json" && report_pass "pod spec: host mount" || report_fail "pod spec: host mount" "missing"

finalize
