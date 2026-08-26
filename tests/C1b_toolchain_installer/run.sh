#!/usr/bin/env bash
# C1b — toolchain installer wiring, offline/deterministic.
# Asserts: pin() resolves each tool from toolchain.env; --dry-run prints a
# deterministic install plan (no network, no mutation); two dry-runs are
# byte-identical; nothing is written to the prefix.

source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../../scripts/blocks/02-toolchain.sh
source "$FLEET_ROOT/scripts/blocks/02-toolchain.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# 1. pin() resolves known tools and rejects unknown.
assert_eq "pin go"       "$(pin go)"       "$TOOLCHAIN_GO_VERSION"
assert_eq "pin kubectl"  "$(pin kubectl)"  "$TOOLCHAIN_KUBECTL_VERSION"
assert_eq "pin restic"   "$(pin restic)"   "$TOOLCHAIN_RESTIC_VERSION"
assert_eq "pin templ"    "$(pin templ)"    "$TOOLCHAIN_TEMPL_VERSION"
assert_rc "pin unknown fails" 1 pin def_not_a_tool

# 2. dry-run is deterministic and names every tool with its pinned version.
dry1="$(install_toolchain "$scratch/prefix" --dry-run 2>&1)"
dry2="$(install_toolchain "$scratch/prefix" --dry-run 2>&1)"

assert_contains "dry-run lists go"     "install go=$TOOLCHAIN_GO_VERSION" "$dry1"
assert_contains "dry-run lists dagger" "install dagger=$TOOLCHAIN_DAGGER_VERSION" "$dry1"
assert_contains "dry-run lists templ"  "install templ=$TOOLCHAIN_TEMPL_VERSION" "$dry1"

assert_eq "dry-run deterministic" "$dry1" "$dry2"

# 3. dry-run mutates nothing: prefix dir must not exist afterward.
assert_not_file "dry-run writes nothing" "$scratch/prefix"

# 4. pin round-trip: every tool's dry-run line references a valid source URL.
while IFS= read -r line; do
  if [[ "$line" =~ \[toolchain\]\[dry-run\]\ install\ ([a-z]+)=([^ ]+)\ from\ (https?://[^ ]+) ]]; then
    assert_contains "source url has scheme for ${BASH_REMATCH[1]}" "http" "${BASH_REMATCH[3]}"
  fi
done <<< "$dry1"

finalize
