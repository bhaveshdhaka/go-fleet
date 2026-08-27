#!/usr/bin/env bash
# C11b — installer path for the fleet CLI (WO-6).
# Static analysis: install.sh wires ci/build-fleet.sh into the prefix with
# the machine contract intact; build-fleet.sh stamps repo VERSION and keeps
# hermetic flags. A real end-to-end install runs in the C1d idempotency
# unit's domain (toolchain network fetch) — here we prove the wiring offline
# plus an actual CLI build into a scratch prefix (no network: go only).

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"

INSTALL="$FLEET_ROOT/install.sh"
assert_file "install.sh present" "$INSTALL"
assert_rc "install.sh syntax" 0 bash -n "$INSTALL"

assert_contains "installer invokes the hermetic builder" \
  'ci/build-fleet.sh' "$(cat "$INSTALL")"
assert_contains "installer targets prefix/bin/fleet" \
  '$prefix/bin/fleet' "$(cat "$INSTALL")"
assert_contains "installer failure contract for CLI build" \
  'FLEET_INSTALL_FAIL reason=fleet_cli_build' "$(cat "$INSTALL")"
assert_contains "installer keeps FLEET_INSTALL_OK contract" \
  'FLEET_INSTALL_OK prefix=' "$(cat "$INSTALL")"
assert_contains "builder keeps hermetic flags" \
  'GOPROXY=off' "$(cat "$FLEET_ROOT/ci/build-fleet.sh")"
assert_contains "builder stamps repo VERSION" \
  'VERSION' "$(cat "$FLEET_ROOT/ci/build-fleet.sh")"

# end-to-end CLI build into a scratch prefix, then run it
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/prefix/bin/fleet" >/dev/null || report_fail "prefix CLI build" "rc!=0"
want_ver="$(tr -d '[:space:]' < "$FLEET_ROOT/VERSION")"
out="$("$scratch/prefix/bin/fleet" version)"
assert_eq "prefix CLI stamps VERSION" "fleet $want_ver" "$out"
[[ -x "$scratch/prefix/bin/fleet" ]] && report_pass "prefix CLI executable" \
  || report_fail "prefix CLI executable" "not executable"

# FLEET_ROOT discovery still works when invoked via absolute path outside repo
out="$(cd "$scratch" && FLEET_ROOT="$FLEET_ROOT" "$scratch/prefix/bin/fleet" status 2>&1)"
assert_contains "CLI resolves repo via explicit FLEET_ROOT" "STATUS SUMMARY components=2" "$out"

finalize
