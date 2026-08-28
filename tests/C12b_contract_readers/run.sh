#!/usr/bin/env bash
# C12b — sos-lab contract readers + ops runner (WO-7), fully hermetic.
# A fake kubectl (on PATH) serves canned cluster answers AND fails hard if
# the child env is not exactly KUBECONFIG=<path> — the rule-7 guarantee is
# exercised with every call. Registry/state/secrets come from fixtures.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
export FLEET_SECRETS_HOME="$scratch/secrets-home"
mkdir -p "$FLEET_SECRETS_HOME"
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

# fake kubectl: canned answers + env discipline. The Go runner passes
# exactly KUBECONFIG=<path>; bash itself injects _/PWD/SHLVL when running
# this script, so only THOSE are allowed — any other variable is a real
# ambient leak (PATH, HOME, tokens...) and fails the call.
mkdir -p "$scratch/bin"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
rest=$(env | sed '/^KUBECONFIG=/d; /^HOME=/d; /^_=/d; /^PWD=/d; /^SHLVL=/d')
if [ -n "$rest" ]; then
  echo "RULE7 VIOLATION: kubectl child env leaked ambient variables:" >&2
  printf '%s\n' "$rest" | sed 's/=.*/=<redacted>/' >&2
  exit 99
fi
case "$*" in
  "get nodes -o wide") printf 'NAME   STATUS   VERSION\nhk-03-dev   Ready   v1.36.3\n' ;;
  "get nodes -o name") echo "node/hk-03-dev" ;;
  "get nodes -o json") echo '{"items":[{"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}' ;;
  *"get pods -o wide") printf 'NAME   READY   STATUS   RESTARTS   AGE\ndemo-1   1/1   Running   0   1h\n' ;;
  *"jsonpath={.items[*].status.phase}"*) echo "Running" ;;
  *"get deployment"*"jsonpath"*) echo "reg/alpha:t1" ;;
  *) echo "unexpected: $*" >&2; exit 97 ;;
esac
FAKE
chmod +x "$scratch/bin/kubectl"

repo="$scratch/repo"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo" -xf -
lab="$scratch/lab-fixture"
mkdir -p "$lab/config" "$lab/state" "$lab/secrets"
cat > "$lab/config/registry.yaml" <<'EOF'
cloudflare:
  account_id: acct123
  tunnel_id: tun123
  tunnel_name: lab
domains:
  example.test:
    zone_id: zone123
services:
  alpha:
    port: 8080
    namespace: sos-lab
    host: alpha.example.test
    enabled: true
    repo: alpha-src
    secrets:
      - ALPHA_KEY
  beta:
    port: 9000
    namespace: other
    image: ghcr.io/x/beta:latest
    secrets: []
EOF
printf '{"alpha": {"tag": "t1", "image": "reg/alpha:t1", "git_sha": "abcdef1234567890"}}\n' > "$lab/state/deployed.json"
printf '{}\n' > "$lab/state/builds.json"
mkdir -p "$FLEET_SECRETS_HOME/hk-03-dev"
printf 'ALPHA_KEY=real-value-never-read\n' > "$FLEET_SECRETS_HOME/hk-03-dev/alpha.env"
: > "$scratch/kubeconfig"

python3 - "$repo" "$lab" "$scratch" <<'EOF'
import re
import sys, pathlib
repo, lab, scratch = sys.argv[1], sys.argv[2], sys.argv[3]
p = pathlib.Path(repo, "ops/SITES.yaml")
t = re.sub(r"lab_root: .*", f"lab_root: {lab}", p.read_text(), count=1)
t = re.sub(r"\n    access: .*", f"\n    access: kubeconfig:{scratch}/kubeconfig", t, count=1)
t = re.sub(r"\n\s*secrets_dir: .*", "", t, count=1)
p.write_text(t)
EOF

run() { PATH="$scratch/bin:$PATH" FLEET_ROOT="$repo" "$F" "$@" 2>&1; }

o="$(run ops status)"
assert_contains "status: services table renders fixture row" "alpha" "$o"
row="$(printf '%s\n' "$o" | grep -E '^alpha +enabled +8080 +t1 +abcdef12 ')"
if [[ -n "$row" ]]; then
  report_pass "status: fixture row columns (state/port/tag/sha)"
else
  report_fail "status: fixture row columns (state/port/tag/sha)" "$(printf '%s\n' "$o" | grep '^alpha' | head -1)"
fi
assert_contains "status: cluster section header" "=== cluster ===" "$o"
assert_contains "status: pods section from fake kubectl" "=== pods (sos-lab) ===" "$o"
if printf '%s' "$o" | grep -q "RULE7 VIOLATION"; then
  report_fail "kubectl child env discipline" "$(printf '%s\n' "$o" | grep -A3 RULE7 | head -4 | tr '\n' ';')"
else
  report_pass "kubectl child env discipline (no ambient vars)"
fi

o="$(run ops doctor)"
assert_contains "doctor: registry parses" "ok    registry.yaml parses" "$o"
assert_contains "doctor: cluster reachable via explicit kubeconfig" "ok    cluster reachable" "$o"
assert_contains "doctor: node ready from canned json" "ok    node ready" "$o"
assert_contains "doctor: platform pods running" "ok    cloudflared running" "$o"
assert_contains "doctor: builder SKIP without crictl" "SKIP  builder image cached — crictl unavailable here — skipped" "$o"
assert_contains "doctor: no cloudflare token -> tunnel check fails loudly" \
  "FAIL  tunnel healthy — missing" "$o"
assert_contains "doctor: deployed image parity from state" "ok    deployed/alpha" "$o"
assert_contains "doctor: footer counts problems" "DOCTOR: 3 problem(s) found" "$o"
if printf '%s' "$o" | grep -q "real-value-never-read"; then
  report_fail "secret values never printed" "value leaked into doctor output"
else
  report_pass "secret values never printed"
fi

# missing secret KEY name (not value) must be named, value never shown
rm "$FLEET_SECRETS_HOME/hk-03-dev/alpha.env"
o="$(run ops doctor)"
assert_contains "doctor: missing secret file named with needed key" \
  "hk-03-dev/alpha.env" "$o"

# broken registry refused cleanly, rc=1
printf 'services: [oops\n' > "$lab/config/registry.yaml"
o="$(run ops status)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"FLEET ERROR :: registry.yaml"* ]]; } \
  && report_pass "broken registry refused cleanly" || report_fail "broken registry refused cleanly" "$rc :: $o"

finalize
