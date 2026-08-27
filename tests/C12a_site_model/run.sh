#!/usr/bin/env bash
# C12a — site model (WO-7): ops/SITES.yaml schema, explicit access modes,
# and the rule-7 guarantee that site resolution never touches ambient state.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"

assert_file "SITES.yaml present" "$FLEET_ROOT/ops/SITES.yaml"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

o="$(bash "$FLEET_ROOT/scripts/fleet" site list 2>&1)"
assert_contains "site list machine line" "SITE LIST count=1" "$o"
assert_contains "hk-03-dev registered" "SITE name=hk-03-dev engine=sos-lab access=in-cluster lab_root=../sos-lab" "$o"

# site data is explicit by construction: the file itself documents that
# access is per-site and ambient fallback is forbidden
assert_contains "SITES.yaml documents explicit access" "is EXPLICIT" "$(cat "$FLEET_ROOT/ops/SITES.yaml")"

# fixture: second site with kubeconfig access parses alongside
repo="$scratch/repo"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo" -xf -
cat >> "$repo/ops/SITES.yaml" <<'EOF'

  - name: drill-vm
    engine: sos-lab
    lab_root: ../sos-lab
    namespace: sos-lab
    access: kubeconfig:.vm/run/kubeconfig
    description: tier-1 drill VM
EOF
o="$(FLEET_ROOT="$repo" "$F" site list 2>&1)"
assert_contains "kubeconfig-access site listed" "SITE name=drill-vm engine=sos-lab access=kubeconfig:.vm/run/kubeconfig" "$o"
assert_contains "both sites listed" "SITE LIST count=2" "$o"

# invalid access mode is surfaced, never silently accepted
repo2="$scratch/repo2"
mkdir -p "$repo2"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo2" -xf -
sed -i 's/^    access: in-cluster$/    access: whatever/' "$repo2/ops/SITES.yaml"
o="$(FLEET_ROOT="$repo2" "$F" site list 2>&1)"
assert_contains "invalid access surfaced" "access=invalid:whatever" "$o"

finalize
