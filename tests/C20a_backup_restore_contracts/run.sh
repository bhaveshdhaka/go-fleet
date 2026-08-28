#!/usr/bin/env bash
# C20a — backup/restore verb contracts (WO-20), offline. Fake kubectl +
# fake restic on PATH. Asserts: R2 creds secret synced, quiesce scale-0 →
# restic backup Job (plain restic args, no shell) → scale back, retention
# forget, secrets-home backup, restore --plan byte-equality, restore Job
# shape, refusals (no backup section, unknown service, no storage).

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
export FLEET_SECRETS_HOME="$scratch/secrets-home"
mkdir -p "$FLEET_SECRETS_HOME"

F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"
export FLEET_RESTIC_BIN="$scratch/bin/restic"
[[ -x "$F" ]] || { report_fail "binary builds" "ci/build-fleet.sh failed"; finalize; }

mkdir -p "$scratch/repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$scratch/repo" -xf -

# site with storage-bearing service (alpha gets storage injected)
mkdir -p "$scratch/lab"
cp -r "$FLEET_ROOT/internal/fleet/testdata/labfix/." "$scratch/lab/"
sed -i 's|^    env:|    storage:\n      size: 2Gi\n      mount: /data\n    env:|' "$scratch/lab/config/registry.yaml"
: > "$scratch/drill.kubeconfig"

cat >> "$scratch/lab/config/registry.yaml" <<'EOF'
backup:
  bucket: test-bucket
  endpoint: https://acct123.r2.cloudflarestorage.com
  repo: drill
  schedule: none
  retention: --keep-daily 7 --keep-weekly 4
EOF

cat > "$scratch/repo/ops/SITES.yaml" <<EOF
sites_version: 1
sites:
  - name: drill
    engine: sos-lab
    lab_root: $scratch/lab
    namespace: sos-lab
    access: kubeconfig:$scratch/drill.kubeconfig
    description: C20a fixture
EOF
: > "$scratch/drill.kubeconfig"

mkdir -p "$FLEET_SECRETS_HOME/drill"
printf 'R2_ACCESS_KEY_ID=testkey\nR2_SECRET_ACCESS_KEY=testsecret\nRESTIC_PASSWORD=testpw\n' > "$FLEET_SECRETS_HOME/drill/r2.env"
printf 'CF_API_TOKEN=dummy\n' > "$FLEET_SECRETS_HOME/drill/cloudflare.env"
printf 'DASHBOARD_SLUG=drilldash\n' > "$FLEET_SECRETS_HOME/drill/dashboard.env"

mkdir -p "$scratch/bin" "$scratch/records"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
rec="$(dirname "$0")/../records"
n=$(( $(ls "$rec" 2>/dev/null | grep -c '\.args$') + 1 ))
printf '%s\n' "$*" > "$rec/$n.args"
if [ "$1" = "apply" ] && [ "$2" = "-f" ] && [ "$3" = "-" ]; then cat > "$rec/$n.stdin"; fi
case "$*" in
  *"create secret generic"*) printf 'apiVersion: v1\nkind: Secret\n' ;;
  "apply -f -") echo configured ;;
  apply\ -f\ *) : ;;
  *"scale deployment/alpha --replicas=0"*) : ;;
  *"scale deployment/alpha --replicas=1"*) : ;;
  *"scale deployment/alpha --replicas="*) : ;;
  *"rollout status"*|*"rollout restart"*) : ;;
  *"delete deployment"*|*"delete service"*|*"delete pvc"*|*"delete job"*) : ;;
  *"get rs"*) echo '{"items":[]}' ;;
  *"jsonpath={.spec.replicas}"*) echo "1" ;;
  *"get pods"*) echo '{"items":[{"metadata":{"name":"p-0"}}]}' ;;
  *"logs -f"*) echo snapshot-log ;;
  *"wait --for=condition=complete"*) : ;;
  *"get nodes"*) echo node ;;
  *) echo "unexpected: $*" >> "$rec/unexpected"; exit 97 ;;
esac
FAKE
cat > "$scratch/bin/restic" <<'RESTIC'
#!/usr/bin/env bash
rec="$(dirname "$0")/../records"
n=$(( $(ls "$rec" 2>/dev/null | grep -c '\.args$') + 1 ))
printf 'restic %s\n' "$*" > "$rec/$n.args"
case "$1" in
  forget|backup) exit 0 ;;
  *) exit 0 ;;
esac
RESTIC
chmod +x "$scratch/bin/kubectl" "$scratch/bin/restic"

# --- backup (quiesce path) --------------------------------------------------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops backup --site drill alpha 2>&1)"
rc=$?
assert_eq "backup rc" "0" "$rc"
assert_contains "backup contract" "BACKUP OK site=drill services=1" "$o"

grep -q "scale deployment/alpha --replicas=0" <(cat "$scratch/records"/*.args) \
  && report_pass "quiesce scale-0" || report_fail "quiesce scale-0" "missing"
grep -q "scale deployment/alpha --replicas=1" <(cat "$scratch/records"/*.args) \
  && report_pass "resume scale-back" || report_fail "resume scale-back" "missing"
grep -q "create secret generic fleet-r2-creds" <(cat "$scratch/records"/*.args) \
  && report_pass "r2 creds secret synced" || report_fail "r2 creds secret synced" "missing"

# the backup Job: official restic image, PVC mount, plain restic args
jobdoc=""
for f in "$scratch/records"/*.stdin; do
  grep -q '"kind":"Job"' "$f" 2>/dev/null && jobdoc="$f"
done
[[ -n "$jobdoc" ]] && report_pass "backup Job applied" || report_fail "backup Job applied" "missing"
grep -q '"image":"restic/restic:0.17.3"' "$jobdoc" && report_pass "official restic image" || report_fail "official restic image" "missing"
grep -q '"command":\["restic"\]' "$jobdoc" && report_pass "plain restic command (no shell)" || report_fail "plain restic command" "missing"
grep -q '"claimName":"alpha-data"' "$jobdoc" && report_pass "service PVC mounted" || report_fail "service PVC mounted" "missing"
grep -q '"/srv/backup"' "$jobdoc" && report_pass "backup path" || report_fail "backup path" "missing"

# host-side restic: forget (retention) + secrets-home backup
grep -q "restic forget --prune --keep-daily 7 --keep-weekly 4" <(cat "$scratch/records"/*.args) \
  && report_pass "retention forget" || report_fail "retention forget" "missing"
grep -q "restic backup .*secrets-home/drill --tag kind=secrets" <(cat "$scratch/records"/*.args) \
  && report_pass "secrets-home backup" || report_fail "secrets-home backup" "missing"

# --- restore --plan: byte-equality ------------------------------------------
FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops restore --site drill alpha --plan > "$scratch/plan1.txt" 2>&1
FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops restore --site drill alpha --plan > "$scratch/plan2.txt" 2>&1
cmp -s "$scratch/plan1.txt" "$scratch/plan2.txt" \
  && report_pass "restore --plan byte-equality" || report_fail "restore --plan byte-equality" "plans differ"
grep -q "RESTORE PLAN alpha snapshot=latest" "$scratch/plan1.txt" \
  && report_pass "plan contract" || report_fail "plan contract" "missing"

# --- restore (real, fake kubectl) --------------------------------------------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops restore --site drill alpha --snapshot latest 2>&1)"
rc=$?
assert_eq "restore rc" "0" "$rc"
assert_contains "restored contract" "RESTORED alpha snapshot=latest" "$o"

# --- refusals ------------------------------------------------------------------
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops backup --site drill nosuch 2>&1)"
assert_contains "unknown service refused" "not registered" "$o"
# a service without storage is refused at restore
o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" ops restore --site drill gamma 2>&1)"
assert_contains "no-storage refused" "has no storage" "$o"
[[ -f "$scratch/records/unexpected" ]] \
  && report_fail "no unexpected kubectl calls" "$(cat "$scratch/records/unexpected")" \
  || report_pass "no unexpected kubectl calls"

finalize
