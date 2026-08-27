#!/usr/bin/env bash
# fleet block 03 — app build pipeline.
#
# Builds the Go apps under apps/ into a dist directory using the pinned Go
# toolchain from toolchain.env (single source of truth). Builds are
# hermetic and deterministic: GOPROXY=off, module mode readonly, trimpath,
# no VCS stamping — repeated builds are byte-identical.
# --dry-run prints the exact plan without mutating anything.
#
# NOTE: real compilation requires the installed toolchain prefix
# (scripts/blocks/02-toolchain.sh). Container tests assert wiring +
# determinism offline; see tests/C3*.

set -uo pipefail
# shellcheck disable=SC1091
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/toolchain.env"

# app_pin -> exact Go compiler version the pipeline must build with.
app_pin() { printf '%s' "${TOOLCHAIN_GO_VERSION}"; }

# find_go -> prints path to a usable go binary, or fails.
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

# build_app <app> <dist-dir> [--dry-run]
build_app() {
  local app=$1 dist_dir=$2 dry_run=false
  shift 2
  for a in "$@"; do [[ "$a" == "--dry-run" ]] && dry_run=true; done

  local want bin_src
  want="$(app_pin)"
  bin_src="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../apps/$app" && pwd)" || {
    echo "ERROR: unknown app '$app'" >&2
    return 1
  }

  local go_bin
  go_bin="$(find_go)" || return 1

  if $dry_run; then
    echo "[pipeline][dry-run] build $app with go=$want"
    echo "[pipeline][dry-run] env GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local"
    echo "[pipeline][dry-run] flags -trimpath -buildvcs=false -ldflags \"-X main.version=$want\""
    echo "[pipeline][dry-run] output ${dist_dir}/${app}"
    return 0
  fi

  mkdir -p "$dist_dir"
  echo "[pipeline] building $app with go=$want ..."

  (
    cd "$bin_src" || exit 1
    export GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local
    # shellcheck disable=SC2086
    exec "$go_bin" build \
      -trimpath -buildvcs=false \
      -ldflags "-X main.version=${want}" \
      -o "${dist_dir}/${app}" .
  ) || return 1

  echo "[pipeline] built ${dist_dir}/${app}"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  APP="${1:?usage: $0 <app> <dist-dir> [--dry-run]}"
  DIST="${2:?usage: $0 <app> <dist-dir> [--dry-run]}"
  shift 2
  build_app "$APP" "$DIST" "$@"
fi
