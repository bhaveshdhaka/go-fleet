#!/usr/bin/env bash
# ci/build-release.sh — static release artifacts for the fleet CLI (WO-6).
#
# Builds CGO_ENABLED=0 static binaries for linux/amd64, darwin/amd64 and
# darwin/arm64 with the same determinism flags as ci/build-fleet.sh
# (GOPROXY=off, trimpath, no VCS stamping), writes them to
# dist/release/fleet_<os>_<arch> plus a SHA256SUMS. Repeat builds are
# byte-identical (asserted by C11a).
#
# Usage: ci/build-release.sh [out-dir]

set -uo pipefail
FLEET_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck disable=SC1091
source "$FLEET_ROOT/toolchain.env"

out_dir="${1:-$FLEET_ROOT/dist/release}"
ver="$(cat "$FLEET_ROOT/VERSION" 2>/dev/null | tr -d '[:space:]')"
[[ -n "$ver" ]] || ver="$TOOLCHAIN_GO_VERSION"

find_go() {
  local candidate="${FLEET_TOOLCHAIN_PREFIX:-}/bin/go"
  if [[ -x "$candidate" ]]; then
    echo "$candidate"
    return 0
  fi
  if command -v go >/dev/null 2>&1; then
    command -v go
    return 0
  fi
  echo "ERROR: no usable go binary (expected toolchain prefix or PATH)" >&2
  return 1
}

go_bin="$(find_go)" || exit 1
mkdir -p "$out_dir"

targets="linux/amd64 darwin/amd64 darwin/arm64"
for t in $targets; do
  os="${t%/*}"; arch="${t#*/}"
  bin="$out_dir/fleet_${os}_${arch}"
  echo "[release] building fleet_${os}_${arch} version=$ver ..."
  (
    cd "$FLEET_ROOT" || exit 1
    export GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local CGO_ENABLED=0
    export GOOS="$os" GOARCH="$arch"
    exec "$go_bin" build \
      -trimpath -buildvcs=false \
      -ldflags "-X main.version=${ver}" \
      -o "$bin" ./cmd/fleet
  ) || exit 1
done

# SHA256SUMS over exactly the release binaries (deterministic order)
(
  cd "$out_dir" || exit 1
  : > SHA256SUMS
  for t in $targets; do
    os="${t%/*}"; arch="${t#*/}"
    sha256sum "fleet_${os}_${arch}" >> SHA256SUMS
  done
)

echo "RELEASE_OK dir=${out_dir#$FLEET_ROOT/} version=$ver targets=$(printf '%s' "$targets" | tr ' ' ',')"
