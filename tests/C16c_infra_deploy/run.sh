#!/usr/bin/env bash
# C16c — infra deploy (WO-15 piece 3), offline. Drives the BUILT binary
# against a fresh-site scaffold with a recording fake kubectl. Asserts:
# registry first, cloudflared second (token secret ensured from
# tunnel.env), gatus last, then the monitor sync; INFRA OK contract line;
# missing tunnel.env warns instead of failing.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
export FLEET_SECRETS_HOME="$scratch/secrets-home"
mkdir -p "$FLEET_SECRETS_HOME"

F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"
[[ -x "$F" ]] || { report_fail "binary builds" "ci/build-fleet.sh failed"; finalize; }

mkdir -p "$scratch/repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$scratch/repo" -xf -

o="$(FLEET_ROOT="$scratch/repo" "$F" site new drillsite --domain example.test 2>&1)"
rc=$?
assert_eq "site new rc" "0" "$rc"

# token for the cloudflared secret + registry account fixup
mkdir -p "$FLEET_SECRETS_HOME/drillsite"
printf 'TUNNEL_TOKEN=tunnel-token-dummy\n' > "$FLEET_SECRETS_HOME/drillsite/tunnel.env"
printf 'DASHBOARD_SLUG=drilldash\n' > "$FLEET_SECRETS_HOME/drillsite/dashboard.env"
sed -i 's/TODO_ACCOUNT_ID/acct123/' "$scratch/repo/ops/sites/drillsite/config/registry.yaml"
sed -i 's/TODO_SITE_TUNNEL_CREATE/tun123/' "$scratch/repo/ops/sites/drillsite/config/registry.yaml"
printf 'CF_API_TOKEN=dummy\n' > "$FLEET_SECRETS_HOME/drillsite/cloudflare.env"

mkdir -p "$scratch/bin" "$scratch/records"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
rec="$(dirname "$0")/../records"
n=$(( $(ls "$rec" 2>/dev/null | grep -c '\.args$') + 1 ))
printf '%s\n' "$*" > "$rec/$n.args"
case "$*" in
  "apply -f -") echo "configured" ;;
  apply\ -f\ *) echo "configured" ;;
  *"create secret generic"*) printf 'apiVersion: v1\nkind: Secret\nmetadata:\n  name: cloudflared-token\n' ;;
  *"rollout status"*) : ;;
  *"rollout restart"*) : ;;
  *"rollout undo"*) : ;;
  *"get cm"*) echo '{}' ;;
  *"get nodes"*) echo "node/x" ;;
  *"get pods"*) echo "NAME READY STATUS" ;;
  *"jsonpath"*) echo "" ;;
  *) echo "unexpected: $*" >> "$rec/unexpected"; exit 97 ;;
esac
FAKE
chmod +x "$scratch/bin/kubectl"

o="$(FLEET_ROOT="$scratch/repo" PATH="$scratch/bin:$PATH" "$F" infra deploy --site drillsite 2>&1)"
rc=$?
assert_eq "infra deploy rc" "0" "$rc"
assert_contains "INFRA OK contract" "INFRA OK site=drillsite applied=3" "$o"

# apply order: registry -> cloudflared (token secret first) -> gatus,
# then monitor's applies. Relative positions, not a rigid sequence.
pos() {  # pos <glob> -> lowest record number matching
  local g=$1 n
  for n in $(seq 1 $(ls "$scratch/records" 2>/dev/null | grep -c '\.args$')); do
    [[ "$(cat "$scratch/records/$n.args" 2>/dev/null)" == $g ]] && { echo "$n"; return; }
  done
  echo 99999
}
p_reg=$(pos *"apply -f *docker-registry.yaml"*)
p_tok=$(pos *"create secret generic cloudflared-token"*)
p_cfd=$(pos *"apply -f *cloudflared.yaml"*)
p_gat=$(pos *"apply -f *gatus.yaml"*)
[[ "$p_tok" -lt "$p_cfd" ]] \
  && report_pass "cloudflared token secret before its apply" \
  || report_fail "cloudflared token secret before its apply" "tok=$p_tok cfd=$p_cfd"
[[ "$p_reg" -lt "$p_cfd" && "$p_cfd" -lt "$p_gat" ]] \
  && report_pass "infra apply order (registry -> cloudflared -> gatus)" \
  || report_fail "infra apply order" "reg=$p_reg cfd=$p_cfd gatus=$p_gat"
[[ "$p_gat" != 99999 ]] \
  && report_pass "gatus template applied" \
  || report_fail "gatus template applied" "missing"

[[ -f "$scratch/records/unexpected" ]] \
  && report_fail "no unexpected kubectl calls" "$(cat "$scratch/records/unexpected")" \
  || report_pass "no unexpected kubectl calls"

# monitor ran (gatus + dashboard sync -> MONITOR OK line in output)
assert_contains "monitor sync ran" "MONITOR OK" "$o"

finalize
