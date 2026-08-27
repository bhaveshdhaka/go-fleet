#!/usr/bin/env bash
# ci/build-fleet.sh — hermetic deterministic build of the Go core (cmd/fleet).
# Mirrors block-03 flags exactly: pinned toolchain, GOPROXY=off, module mode
# readonly, trimpath, no VCS stamping. Repeat builds are byte-identical
# (asserted by C9a). Usage: ci/build-fleet.sh [output-path]

set -uo pipefail
FLEET_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck disable=SC1091
source "$FLEET_ROOT/toolchain.env"

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

out="${1:-$FLEET_ROOT/dist/fleet}"
go_bin="$(find_go)" || exit 1
mkdir -p "$(dirname "$out")"

(
  cd "$FLEET_ROOT" || exit 1
  export GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local
  exec "$go_bin" build \
    -trimpath -buildvcs=false \
    -ldflags "-X main.version=${TOOLCHAIN_GO_VERSION}" \
    -o "$out" ./cmd/fleet
) || exit 1

echo "BUILT ${out#$FLEET_ROOT/} go=${TOOLCHAIN_GO_VERSION}"
