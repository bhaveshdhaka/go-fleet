#!/usr/bin/env bash
# C12c — ops doctor --json shape and lifecycle warnings (WO-7).
# Hermetic: fake kubectl serves canned cluster answers; cloudflare fails
# without a token (asserted); a repo service with a newer build than its
# deploy yields a lifecycle WARN; the JSON records mirror the frozen
# contract (check/ok/detail/status keys, exact values). Shape asserts run
# through tests/lib/jsonq (stdlib Go) — no interpreted runtimes.

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

# point the copied site registry at the scratch fixture (plain line edits)
sed -i -e "s|^    lab_root: .*|    lab_root: $lab|" \
       -e "s|^    access: .*|    access: kubeconfig:$scratch/kubeconfig|" \
       "$repo/ops/SITES.yaml"

PATH="$scratch/bin:$PATH" FLEET_ROOT="$repo" "$F" ops doctor --json > "$scratch/doctor.json" 2>&1

jsonq_build "$scratch/jsonq" || { report_fail "jsonq builds" "go build failed"; finalize; }
J="$scratch/jsonq"
D="$scratch/doctor.json"

t="$("$J" "$D" type . 2>&1)"
l="$("$J" "$D" len . 2>&1)"
if [[ "$t" == "list" ]] && [[ "$l" -gt 10 ]]; then
  report_pass "doctor --json list shape (>10 checks)"
else
  report_fail "doctor --json list shape (>10 checks)" "type=$t len=$l json=$(head -c 120 "$D")"
fi

u="$("$J" "$D" keys-each . 2>/dev/null | sort -u)"
if [[ "$u" == "check,detail,ok,status" ]]; then
  report_pass "check objects carry exactly check/ok/detail/status"
else
  report_fail "check objects carry exactly check/ok/detail/status" "keys: $u"
fi

[[ "$("$J" "$D" count . check=parity 2>/dev/null)" == "0" ]] \
  && report_pass "no parity checks when undeclared" \
  || report_fail "no parity checks when undeclared" "parity check present"

st="$("$J" "$D" find . check=lifecycle/alpha status 2>/dev/null)"
det="$("$J" "$D" find . check=lifecycle/alpha detail 2>/dev/null)"
if [[ "$st" == "warn" && "$det" == *"build t2 not deployed yet"* ]]; then
  report_pass "lifecycle/alpha warns on stale build"
else
  report_fail "lifecycle/alpha warns on stale build" "status=$st detail=$det"
fi

[[ "$("$J" "$D" find . check=deployed/alpha status 2>/dev/null)" == "ok" ]] \
  && report_pass "deployed/alpha parity ok" \
  || report_fail "deployed/alpha parity ok" "not ok"

fails="$("$J" "$D" count . status=fail 2>/dev/null)"
if [[ "$fails" -ge 3 ]]; then
  report_pass "cloudflare failures present ($fails)"
else
  report_fail "cloudflare failures present" "only ${fails:-0} fail-status checks"
fi

finalize
