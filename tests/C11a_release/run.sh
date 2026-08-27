#!/usr/bin/env bash
# C11a — release artifacts (WO-6): static cross builds are byte-identical
# on repeat, SHA256SUMS verifies, and every artifact stamps the repo
# VERSION. darwin binaries cannot run here — checksums + file type only.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../../toolchain.env
source "$FLEET_ROOT/toolchain.env"

assert_file "build-release.sh present" "$FLEET_ROOT/ci/build-release.sh"
assert_rc "build-release.sh syntax" 0 bash -n "$FLEET_ROOT/ci/build-release.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
bash "$FLEET_ROOT/ci/build-release.sh" "$scratch/rel1" >/dev/null || report_fail "release build 1" "rc!=0"
bash "$FLEET_ROOT/ci/build-release.sh" "$scratch/rel2" >/dev/null || report_fail "release build 2" "rc!=0"

want_ver="$(tr -d '[:space:]' < "$FLEET_ROOT/VERSION")"
for t in linux_amd64 darwin_amd64 darwin_arm64; do
  assert_file "artifact fleet_$t" "$scratch/rel1/fleet_$t"
done
assert_file "SHA256SUMS present" "$scratch/rel1/SHA256SUMS"

h1="$(cd "$scratch/rel1" && sha256sum fleet_linux_amd64 fleet_darwin_amd64 fleet_darwin_arm64)"
h2="$(cd "$scratch/rel2" && sha256sum fleet_linux_amd64 fleet_darwin_amd64 fleet_darwin_arm64)"
assert_eq "repeat release builds byte-identical" "$h1" "$h2"

( cd "$scratch/rel1" && sha256sum -c SHA256SUMS >/dev/null 2>&1 )
if [[ $? -eq 0 ]]; then
  report_pass "SHA256SUMS verifies all artifacts"
else
  report_fail "SHA256SUMS verifies all artifacts" "sha256sum -c failed"
fi

# linux artifact is a static ELF stamped with the repo version
magic="$(od -An -tx1 -N4 "$scratch/rel1/fleet_linux_amd64" 2>/dev/null | tr -d ' \n')"
assert_eq "linux artifact is ELF (magic 7f454c46)" "7f454c46" "$magic"
assert_contains "release builder forces CGO_ENABLED=0" "CGO_ENABLED=0" "$(cat "$FLEET_ROOT/ci/build-release.sh")"
out="$("$scratch/rel1/fleet_linux_amd64" version)"
assert_eq "linux artifact stamps repo VERSION" "fleet $want_ver" "$out"

n_sums="$(grep -c . "$scratch/rel1/SHA256SUMS")"
assert_eq "SHA256SUMS covers every target" 3 "$n_sums"

finalize
