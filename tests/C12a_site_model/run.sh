#!/usr/bin/env bash
# C12a — site model (WO-7, structural since WO-9): ops/SITES.yaml schema,
# explicit access modes, engine vocabulary, and the rule-7 guarantee that
# site resolution never touches ambient state. Asserts the MACHINE
# CONTRACT of `site list`, not live site data (live data changes at
# WO-9 cutover and later).

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"

assert_file "SITES.yaml present" "$FLEET_ROOT/ops/SITES.yaml"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

# live registry: at least one site, every entry well-formed, engine in
# the supported vocabulary, access explicit
o="$(bash "$FLEET_ROOT/scripts/fleet" site list 2>&1)"
assert_contains "site list machine line" "SITE LIST count=" "$o"
bad=0
while IFS= read -r ln; do
  case "$ln" in
    SITE\ name=*)
      case "$ln" in
        *engine=sos-lab*|*engine=fleet*) : ;;
        *) bad=1 ;;
      esac
      case "$ln" in
        *access=in-cluster*|*access=kubeconfig:*) : ;;
        *) bad=1 ;;
      esac
      case "$ln" in
        *lab_root=*) : ;;
        *) bad=1 ;;
      esac
      ;;
  esac
done <<< "$o"
if [[ $bad -eq 0 ]] && grep -q "^  - name: " "$FLEET_ROOT/ops/SITES.yaml"; then
  report_pass "all live sites well-formed (engine+access+lab_root)"
else
  report_fail "all live sites well-formed (engine+access+lab_root)" "malformed SITE line or empty registry"
fi

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
n_now="$(printf '%s\n' "$o" | grep -c '^SITE name=')"
n_base="$(bash "$FLEET_ROOT/scripts/fleet" site list 2>&1 | grep -c '^SITE name=')"
assert_eq "both sites listed (base+1)" "$((n_base + 1))" "$n_now"

# invalid access mode is surfaced, never silently accepted
repo2="$scratch/repo2"
mkdir -p "$repo2"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo2" -xf -
sed -i '0,/    access: /s//    access: whatever/' "$repo2/ops/SITES.yaml"
o="$(FLEET_ROOT="$repo2" "$F" site list 2>&1)"
assert_contains "invalid access surfaced" "access=invalid:whatever" "$o"

# unsupported engine is refused by the ops surface (rule: engine vocabulary)
sed -i '0,/    engine: /s//    engine: future-thing/' "$repo2/ops/SITES.yaml"
o="$(FLEET_ROOT="$repo2" "$F" ops status 2>&1)"
assert_contains "unsupported engine refused" "not supported yet" "$o"

finalize
