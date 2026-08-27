#!/usr/bin/env bash
# C3b — pipeline dry-run contract.
# Asserts --dry-run prints a deterministic plan naming the pinned Go version,
# two invocations are byte-identical, and nothing is mutated (dist-dir must
# not be created). Offline: no go invocation at all.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"
# shellcheck source=scripts/blocks/03-pipeline.sh
source "$FLEET_ROOT/scripts/blocks/03-pipeline.sh"

want="$TOOLCHAIN_GO_VERSION"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
dist_dir="$scratch/dist"

dry1="$(build_app fleetctl "$dist_dir" --dry-run 2>&1)"
dry2="$(build_app fleetctl "$dist_dir" --dry-run 2>&1)"

assert_contains "dry-run names app+pin" "[pipeline][dry-run] build fleetctl with go=$want" "$dry1"
assert_contains "dry-run sets GOPROXY off" "GOPROXY=off" "$dry1"
assert_contains "dry-run names output path" "output ${dist_dir}/fleetctl" "$dry1"
assert_eq "dry-run byte-identical" "$dry1" "$dry2"

# dist-dir must not have been created by either dry run.
assert_not_file "dry-run writes nothing" "$dist_dir"

finalize
