#!/usr/bin/env bash
# fleet install.sh — one-command bootstrap for humans AND agents.
#
#   ./install.sh [--verify] [--json] [--prefix DIR] [--force]
#
# Installs every pinned tool from toolchain.env into the repo-local
# prefix (default ./.toolchain) using scripts/blocks/02-toolchain.sh.
# No sudo. Idempotent: existing pinned versions are skipped.
# Machine contract for agents:
#   success -> single line  FLEET_INSTALL_OK prefix=<abs>
#   failure -> single line  FLEET_INSTALL_FAIL reason=<short>   (rc=1)

set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT" || exit 1
export FLEET_ROOT="$ROOT"
# shellcheck disable=SC1091
source "$ROOT/toolchain.env"

verify=false json=false force=false prefix="$ROOT/.toolchain"
for a in "$@"; do
  case "$a" in
    --verify) verify=true ;;
    --json) json=true ;;
    --prefix) : ;; # handled by shift below
    --force) force=true ;;
    *) echo "unknown flag $a (see header)" >&2; exit 2 ;;
  esac
done
while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) [[ $# -ge 2 ]] && { prefix=$(cd "$2" && pwd); shift; } ;;
    esac
  shift
done

say() { $json && return 0; printf '%s\n' "$*"; }

need() { command -v "$1" >/dev/null 2>&1 || return 1; }
missing=""
for t in bash curl git tar gzip; do
  need "$t" || missing+="$t "
done
if [[ -n "$missing" ]]; then
  $json && echo "FLEET_INSTALL_FAIL reason=missing_tools:$missing" || \
    say "FLEET INSTALL FAIL — missing tools: $missing"
  exit 1
fi
say "[install] prerequisites ok"
command -v bzip2 >/dev/null 2>&1 \
  || say "[install] note: bzip2 not found — restic will be skipped (everything else installs)"

FORCE_FLAG=""; $force && FORCE_FLAG="--force"
if ! out="$(bash -c "
  source '$ROOT/scripts/blocks/02-toolchain.sh'
  install_toolchain '$prefix' $FORCE_FLAG
" 2>&1)"; then
  $json && echo "FLEET_INSTALL_FAIL reason=toolchain_install"
  say "FLEET INSTALL FAIL"; printf '%s\n' "$out"; exit 1
fi
printf '%s\n' "$out"

export PATH="$prefix/bin:$PATH"
got="$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')"
if [[ "$got" != "$TOOLCHAIN_GO_VERSION" ]]; then
  $json && echo "FLEET_INSTALL_FAIL reason=go_version_mismatch:got=${got:-none}"
  say "FLEET INSTALL FAIL — go ${got:-missing} != pinned $TOOLCHAIN_GO_VERSION"
  exit 1
fi
say "[install] verified go $got at $prefix/bin/go"

# WO-6: install the fleet control-plane CLI itself into the prefix. The
# binary is built hermetically (ci/build-fleet.sh: pinned toolchain,
# GOPROXY=off, trimpath, byte-reproducible) and stamped with repo VERSION.
if ! out="$(bash "$ROOT/ci/build-fleet.sh" "$prefix/bin/fleet" 2>&1)"; then
  $json && echo "FLEET_INSTALL_FAIL reason=fleet_cli_build"
  say "FLEET INSTALL FAIL — fleet CLI build failed"; printf '%s\n' "$out"; exit 1
fi
printf '%s\n' "$out" | sed 's/^/[install] /'
say "[install] fleet CLI at $prefix/bin/fleet ($("$prefix/bin/fleet" version))"

# Partial installs are acceptable when only non-core tools failed (e.g.
# restic without bzip2 in slim containers). Core = whatever the corpus needs:
# go + kubectl presence decide honesty here; report machine-parseable status.
summary_line="$(printf '%s\n' "$out" | grep -oE '^errors=[0-9]+ failed_tools:.*$' | tail -1)"
failed="$(printf '%s' "$summary_line" | sed 's/^errors=[0-9]* failed_tools: //; s/^none$//')"
noncritical=" restic"
if [[ -n "$(printf '%s' "$failed" | tr -d "$noncritical" | tr -d ' ')" ]]; then
  $json && echo "FLEET_INSTALL_FAIL reason=critical_tool_failure:$failed"
  say "FLEET INSTALL FAIL — critical tools failed:$failed"; exit 1
fi

rc=0
if $verify; then
  say "[install] running hermetic test corpus (--verify)"
  bash "$ROOT/scripts/test.sh" || rc=1
fi

if [[ $rc -eq 0 ]]; then
  $json && echo "FLEET_INSTALL_OK prefix=$prefix"
  say "[install] next steps:"
  say "  ./scripts/fleet status          # what is registered/deployed"
  say "  ./scripts/fleet doctor          # health/drift check (read-only)"
  say "  cat AGENTS.md                   # operating rules (agents: start here)"
else
  $json && echo "FLEET_INSTALL_FAIL reason=test_corpus"
fi
exit $rc
