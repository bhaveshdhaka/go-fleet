#!/usr/bin/env bash
# C9a — Go core build integrity (WO-4).
# Root module pins the toolchain Go version; cmd/fleet vets clean offline;
# ci/build-fleet.sh produces byte-identical binaries on repeat builds and
# stamps the pinned version; templates ship via embed.FS. No network.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"

assert_file "go.mod present" "$FLEET_ROOT/go.mod"
assert_file "cmd/fleet/main.go present" "$FLEET_ROOT/cmd/fleet/main.go"
assert_file "build-fleet.sh present" "$FLEET_ROOT/ci/build-fleet.sh"

assert_rc "build-fleet.sh syntax" 0 bash -n "$FLEET_ROOT/ci/build-fleet.sh"
assert_rc "scripts/fleet shim syntax" 0 bash -n "$FLEET_ROOT/scripts/fleet"
assert_rc "ci/promote.sh shim syntax" 0 bash -n "$FLEET_ROOT/ci/promote.sh"

got="$(awk 'tolower($1)=="go"{print $2}' "$FLEET_ROOT/go.mod")"
assert_eq "go.mod pins TOOLCHAIN_GO_VERSION" "$TOOLCHAIN_GO_VERSION" "$got"

# offline vet of the whole module
(
  cd "$FLEET_ROOT" || exit 1
  export GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local
  go vet ./... >/dev/null 2>&1
)
if [[ $? -eq 0 ]]; then
  report_pass "go vet ./... clean (offline)"
else
  report_fail "go vet ./... clean (offline)" "vet returned non-zero"
fi

# hermetic deterministic build: two independent builds, byte-identical
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet1" >/dev/null || report_fail "build 1" "rc!=0"
bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet2" >/dev/null || report_fail "build 2" "rc!=0"
h1="$(sha256sum "$scratch/fleet1" | cut -d' ' -f1)"
h2="$(sha256sum "$scratch/fleet2" | cut -d' ' -f1)"
assert_eq "repeat builds byte-identical" "$h1" "$h2"

ver="$("$scratch/fleet1" version)"
want_ver="$(tr -d '[:space:]' < "$FLEET_ROOT/VERSION")"
assert_eq "binary stamps repo VERSION" "fleet $want_ver" "$ver"

# templates ship inside the binary: init must work from ANY cwd
proj="$scratch/proj"
out="$(cd "$scratch" && FLEET_ROOT= "$scratch/fleet1" init "$proj" 2>&1)"
assert_contains "init works cwd-independent" "INIT OK" "$out"
assert_file "init wrote registry" "$proj/ops/PROJECTS.yaml"
assert_file "init wrote gates" "$proj/lifecycle/gates.yaml"
assert_file "init wrote journal" "$proj/lifecycle/journal/events.log"

finalize
