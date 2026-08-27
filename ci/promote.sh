#!/usr/bin/env bash
# ci/promote.sh — thin shim over the Go core (WO-4).
# The gate engine moved into cmd/fleet promote; this preserves the
# historical entrypoint and its rc/stdout/stderr contract for existing
# callers (the corpus drives it via this path in C5c).

set -uo pipefail
FLEET_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export FLEET_ROOT

exec bash "$FLEET_ROOT/scripts/fleet" promote "$@"
