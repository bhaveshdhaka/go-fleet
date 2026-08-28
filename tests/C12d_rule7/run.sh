#!/usr/bin/env bash
# C12d — rule-7 discipline for the ops engine (WO-7).
# 1. The Go core never copies the ambient environment into any child
#    process (no os.Environ() anywhere; kubectl env is constructed).
# 2. in-cluster access REFUSES to run outside the declared environment
#    (unset KUBERNETES_SERVICE_HOST -> hard error, never a fallback).
# 3. kubeconfig: sites require the exact declared file; unknown sites
#    are refused.

source "$FLEET_ROOT/scripts/lib.sh"

GO_SRC="$(cat "$FLEET_ROOT"/internal/fleet/*.go "$FLEET_ROOT"/cmd/fleet/*.go 2>/dev/null)"

if printf '%s' "$GO_SRC" | grep -q "os\.Environ()"; then
  report_fail "no ambient environment copying" "os.Environ() found in Go core"
else
  report_pass "no ambient environment copying"
fi
assert_contains "kubectl child env constructed explicitly" \
  '"KUBECONFIG=" + r.kubeconfig' \
  "$(cat "$FLEET_ROOT/internal/fleet/kubectl.go")"
assert_contains "in-cluster refusal documented in code" \
  "refusing to fall back to ambient credentials" \
  "$(cat "$FLEET_ROOT/internal/fleet/kubectl.go")"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

repo="$scratch/repo"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo" -xf -

# fixture lab at <scratch>/sos-lab so the UNCHANGED lab_root resolves
lab="$scratch/sos-lab"
mkdir -p "$lab/config" "$lab/state"
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
    image: reg/alpha:t1
EOF
printf '{}\n' > "$lab/state/deployed.json"
printf '{}\n' > "$lab/state/builds.json"

# 1. in-cluster declared but no in-cluster env -> hard refuse
env -u KUBERNETES_SERVICE_HOST FLEET_ROOT="$repo" "$F" ops status > "$scratch/out" 2>&1
rc=$?
if [[ $rc -eq 1 ]] && grep -q "refusing to fall back to ambient credentials" "$scratch/out"; then
  report_pass "in-cluster without SA env refuses (no ambient fallback)"
else
  report_fail "in-cluster without SA env refuses (no ambient fallback)" \
    "rc=$rc :: $(head -2 "$scratch/out")"
fi

# 2. kubeconfig site with missing file -> named error
sed -i "s|^    access: .*|    access: kubeconfig:$scratch/missing/kubeconfig|" "$repo/ops/SITES.yaml"
o="$(FLEET_ROOT="$repo" "$F" ops status 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"kubeconfig missing"* ]]; } \
  && report_pass "missing kubeconfig named, no fallback" || report_fail "missing kubeconfig named, no fallback" "$rc :: $o"

# 3. unknown site refused
sed -i "s|^    access: .*|    access: in-cluster|" "$repo/ops/SITES.yaml"
o="$(FLEET_ROOT="$repo" "$F" ops status --site ghost 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"unknown site 'ghost'"* ]]; } \
  && report_pass "unknown site refused" || report_fail "unknown site refused" "$rc :: $o"

finalize
