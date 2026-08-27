#!/usr/bin/env bash
# C6a — fleethub hermetic build reproducibility.
# Uses the shared pipeline block: same contract as fleetctl builds.
# Offline (stdlib-only), binary stamps the pinned Go version.

source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"

if ! command -v go >/dev/null 2>&1; then
  report_skip "go available" "toolchain not installed"
  finalize
  exit 0
fi
# shellcheck source=scripts/blocks/03-pipeline.sh
source "$FLEET_ROOT/scripts/blocks/03-pipeline.sh"

want="$TOOLCHAIN_GO_VERSION"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
dist_dir="$scratch/dist"

build_app fleethub "$dist_dir" >"$scratch/build.log" 2>&1
rc=$?
[[ $rc -eq 0 ]] && report_pass "offline build rc=0" \
                || { report_fail "offline build rc=0" "rc=$rc :: $(cat "$scratch/build.log")"; finalize; exit 1; }

binary="$dist_dir/fleethub"
assert_file "binary built" "$binary"

out="$("$binary" version 2>&1)"
assert_eq "binary reports pinned Go version" "fleethub $want" "$out"

assert_zero_delta "two builds byte-identical" "$dist_dir" \
  build_app fleethub "$dist_dir"

finalize
