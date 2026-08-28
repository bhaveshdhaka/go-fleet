#!/usr/bin/env bash
# C14a — site migration (WO-9 piece 1; secrets home since WO-14), offline.
# `fleet site init <name> --from <lab_root>` must: import registry/state/
# templates byte-for-byte, write an archive + MIGRATION manifest, COPY the
# predecessor's secrets/*.env into the fleet secrets home (0600, source
# untouched), cutover the SITES.yaml entry (engine: fleet, NO secrets_dir
# — the override is deleted from the schema), refuse re-init and invalid
# sources, and leave the ops verbs a parseable site.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
export FLEET_SECRETS_HOME="$scratch/secrets-home"
mkdir -p "$FLEET_SECRETS_HOME"

F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"
[[ -x "$F" ]] || { report_fail "binary builds" "ci/build-fleet.sh failed"; finalize; }

mkdir -p "$scratch/repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$scratch/repo" -xf -
mkdir -p "$scratch/fixture"
cp -r "$FLEET_ROOT/internal/fleet/testdata/labfix/." "$scratch/fixture/"
SECRET_TAIL="7f3a9"
SECRET_VALUE="do-not-copy-me-${SECRET_TAIL}"
printf 'SECRET_VALUE=%s\n' "$SECRET_VALUE" > "$scratch/fixture/secrets/alpha.env"

o="$(FLEET_ROOT="$scratch/repo" "$F" site init drillsite --from "$scratch/fixture" 2>&1)"
rc=$?
assert_eq "site init rc=0" "0" "$rc"
assert_contains "site init contract line" "SITE INIT site=drillsite engine=fleet" "$o"
assert_contains "secrets copied count" "secrets_copied=2" "$o"

assert_file "registry imported" "$scratch/repo/ops/sites/drillsite/config/registry.yaml"
cmp -s "$scratch/repo/ops/sites/drillsite/config/registry.yaml" "$scratch/fixture/config/registry.yaml" \
  && report_pass "registry byte-preserved" \
  || report_fail "registry byte-preserved" "differs from source"
assert_file "state imported" "$scratch/repo/ops/sites/drillsite/state/deployed.json"
assert_file "templates imported" "$scratch/repo/ops/sites/drillsite/templates/dashboard-nginx.conf"
assert_file "archive manifest" "$scratch/repo/ops/sites/drillsite/archive/MIGRATION.md"
assert_file "archive deployed snapshot" "$scratch/repo/ops/sites/drillsite/archive/deployed.json"
grep -q "secrets: 2 env files copied" "$scratch/repo/ops/sites/drillsite/archive/MIGRATION.md" \
  && report_pass "manifest records secrets copied" \
  || report_fail "manifest records secrets copied" "missing"

# secret file relocated into the secrets home, 0600, value intact
assert_file "secret in secrets home" "$FLEET_SECRETS_HOME/drillsite/alpha.env"
perm=$(stat -c '%a' "$FLEET_SECRETS_HOME/drillsite/alpha.env")
[[ "$perm" == "600" ]] \
  && report_pass "secret file mode 0600" \
  || report_fail "secret file mode 0600" "$perm"
perm=$(stat -c '%a' "$FLEET_SECRETS_HOME/drillsite")
[[ "$perm" == "700" ]] \
  && report_pass "secrets home dir mode 0700" \
  || report_fail "secrets home dir mode 0700" "$perm"

# SITES.yaml entry appended with engine fleet and NO secrets_dir key
grep -A5 "  - name: drillsite" "$scratch/repo/ops/SITES.yaml" | grep -q "engine: fleet" \
  && report_pass "SITES.yaml: engine fleet" \
  || report_fail "SITES.yaml: engine fleet" "missing"
grep -A6 "  - name: drillsite" "$scratch/repo/ops/SITES.yaml" | grep -q "secrets_dir" \
  && report_fail "SITES.yaml: no secrets_dir key" "override key present" \
  || report_pass "SITES.yaml: no secrets_dir key"

# SECRET VALUE must not exist anywhere under the fleet tree
if grep -rq "$SECRET_VALUE" "$scratch/repo"; then
  report_fail "no secret values in fleet tree" "fixture secret value found under repo"
else
  report_pass "no secret values in fleet tree"
fi

# the SOURCE secret file is untouched (WO-9 constraint preserved)
grep -q "$SECRET_VALUE" "$scratch/fixture/secrets/alpha.env" \
  && report_pass "source secret untouched" \
  || report_fail "source secret untouched" "source modified"

# site list shows the migrated site; ops verbs accept engine fleet
o="$(FLEET_ROOT="$scratch/repo" "$F" site list 2>&1)"
assert_contains "site list: drillsite fleet" "SITE name=drillsite engine=fleet" "$o"

# re-init refuses
o="$(FLEET_ROOT="$scratch/repo" "$F" site init drillsite --from "$scratch/fixture" 2>&1)"
assert_contains "re-init refused" "already exists" "$o"

# in-place cutover of an EXISTING sos-lab-engine entry
cat >> "$scratch/repo/ops/SITES.yaml" <<EOF
  - name: drillsec
    engine: sos-lab
    lab_root: $scratch/fixture
    namespace: sos-lab
    access: in-cluster
    description: second fixture site
EOF
o="$(FLEET_ROOT="$scratch/repo" "$F" site init drillsec --from "$scratch/fixture" 2>&1)"
rc=$?
assert_eq "in-place cutover rc=0" "0" "$rc"
sed -n '/  - name: drillsec/,/description/p' "$scratch/repo/ops/SITES.yaml" > "$scratch/sec-entry.txt"
grep -q "engine: fleet" "$scratch/sec-entry.txt" \
  && report_pass "existing entry flipped to fleet" \
  || report_fail "existing entry flipped to fleet" "$(cat "$scratch/sec-entry.txt")"
grep -q "lab_root: ops/sites/drillsec" "$scratch/sec-entry.txt" \
  && report_pass "existing entry lab_root repointed" \
  || report_fail "existing entry lab_root repointed" "missing"

# invalid source refused (corrupted registry), no site dir left behind
mkdir -p "$scratch/broken/config" "$scratch/broken/templates"
printf 'services: [unclosed\n' > "$scratch/broken/config/registry.yaml"
o="$(FLEET_ROOT="$scratch/repo" "$F" site init broken --from "$scratch/broken" 2>&1)"
assert_contains "corrupt source refused by validation" "imported registry failed validation" "$o"
# broken has the dir but no templates? it HAS templates dir; registry is corrupt but EXISTS
# -> import runs, validation must fail, dir must be removed
[[ -d "$scratch/repo/ops/sites/broken" ]] \
  && report_fail "failed import leaves no site dir" "ops/sites/broken exists" \
  || report_pass "failed import leaves no site dir"

finalize
