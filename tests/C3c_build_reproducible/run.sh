#!/usr/bin/env bash
# C3c — real offline build reproducibility.
# Requires the installed toolchain (dep: C1b). Builds fleetctl for real with
# GOPROXY=off, asserts the binary runs and reports the pinned version, and
# that two consecutive builds are byte-identical in the dist tree
# (assert_zero_delta). No network: module is stdlib-only.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"

if ! command -v go >/dev/null 2>&1; then
  report_skip "go available" "toolchain not installed — installer tier needed"
  finalize
  exit 0
fi

# shellcheck source=scripts/blocks/03-pipeline.sh
source "$FLEET_ROOT/scripts/blocks/03-pipeline.sh"

want="$TOOLCHAIN_GO_VERSION"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
dist_dir="$scratch/dist"

build_app fleetctl "$dist_dir" >"$scratch/build.log" 2>&1
rc=$?
if [[ $rc -eq 0 ]]; then
  report_pass "offline build rc=0"
else
  report_fail "offline build rc=0" "rc=$rc log: $(cat "$scratch/build.log")"
  finalize
  exit 1
fi

binary="$dist_dir/fleetctl"
assert_file "binary built" "$binary"

out="$("$binary" version 2>&1)"
assert_eq "binary reports pinned version" "fleetctl $want" "$out"

# idempotency/reproducibility: second real build leaves dist byte-identical.
assert_zero_delta "two builds byte-identical" "$dist_dir" \
  build_app fleetctl "$dist_dir"

finalize
