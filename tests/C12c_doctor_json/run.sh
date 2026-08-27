#!/usr/bin/env bash
# C12c — ops doctor --json shape and lifecycle warnings (WO-7).
# Hermetic: fake kubectl serves canned cluster answers; cloudflare fails
# without a token (asserted); a repo service with a newer build than its
# deploy yields a lifecycle WARN; the JSON records mirror the python
# contract (check/ok/detail/status keys, exact values).

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

mkdir -p "$scratch/bin"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
case "$*" in
  "get nodes -o wide") printf 'NAME   STATUS\nhk-03-dev   Ready\n' ;;
  "get nodes -o name") echo "node/hk-03-dev" ;;
  "get nodes -o json") echo '{"items":[{"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}' ;;
  *"get pods -o wide") printf 'NAME   READY   STATUS\ndemo-1   1/1   Running\n' ;;
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
    image: reg/alpha:t1
EOF
printf '{"alpha": {"tag": "t1", "image": "reg/alpha:t1", "git_sha": "abcdef1234567890"}}\n' > "$lab/state/deployed.json"
printf '{"alpha": {"tag": "t2", "git_sha": "ffffffffffffffff", "built_at": "2026-08-27T00:00:00Z"}}\n' > "$lab/state/builds.json"
: > "$scratch/kubeconfig"

python3 - "$repo" "$lab" "$scratch" <<'EOF'
import sys, pathlib
repo, lab, scratch = sys.argv[1], sys.argv[2], sys.argv[3]
p = pathlib.Path(repo, "ops/SITES.yaml")
p.write_text(p.read_text().replace("lab_root: ../sos-lab", f"lab_root: {lab}").replace("access: in-cluster", f"access: kubeconfig:{scratch}/kubeconfig"))
EOF

PATH="$scratch/bin:$PATH" FLEET_ROOT="$repo" "$F" ops doctor --json > "$scratch/doctor.json" 2>&1

python3_out="$(python3 - "$scratch/doctor.json" <<'EOF'
import json, sys
checks = json.load(open(sys.argv[1]))
ok = True
by = {c["check"]: c for c in checks}
def want(cond, msg):
    global ok
    if not cond:
        ok = False
        print("FAIL-" + msg)
want(isinstance(checks, list) and len(checks) > 10, "list-shape")
for c in checks:
    want(set(c.keys()) == {"check", "ok", "detail", "status"}, "keys")
want(by.get("parity") is None, "no-parity-when-undeclared")
lc = by.get("lifecycle/alpha")
want(lc is not None and lc["status"] == "warn" and "build t2 not deployed yet" in lc["detail"], "lifecycle-warn")
want(by.get("deployed/alpha", {}).get("status") == "ok", "deployed-parity")
fails = [c for c in checks if c["status"] == "fail"]
want(len(fails) >= 3, "cloudflare-fails-present")
print("C12C_JSON_OK" if ok else "C12C_JSON_BAD")
EOF
)"
assert_contains "doctor --json mirrors python contract" "C12C_JSON_OK" "$python3_out"

finalize
