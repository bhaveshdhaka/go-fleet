#!/usr/bin/env bash
# C3a — Go app scaffold + pipeline block static analysis.
# Asserts apps/fleetctl wiring against toolchain.env and 03-pipeline.sh
# syntax/pin plumbing, offline. No network, no mutation outside scratch.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"
# shellcheck source=scripts/blocks/03-pipeline.sh
source "$FLEET_ROOT/scripts/blocks/03-pipeline.sh"

BLOCK="$FLEET_ROOT/scripts/blocks/03-pipeline.sh"
APP_DIR="$FLEET_ROOT/apps/fleetctl"

# ── scaffold presence ──────────────────────────────────────────────────────
assert_file "go.mod present"      "$APP_DIR/go.mod"
assert_file "main.go present"     "$APP_DIR/main.go"

# ── pipeline block syntax + pin plumbing ───────────────────────────────────
assert_rc "pipeline block bash -n" 0 bash -n "$BLOCK"
assert_eq "app_pin matches toolchain.env" "$TOOLCHAIN_GO_VERSION" "$(app_pin)"

# ── go.mod pins exactly the pinned compiler version ────────────────────────
got="$(awk 'tolower($1)=="go"{print $2}' "$APP_DIR/go.mod")"
assert_eq "go.mod pins TOOLCHAIN_GO_VERSION" "$TOOLCHAIN_GO_VERSION" "$got"

# ── module compiles clean: go vet (stdlib-only, so fully offline) ──────────
(
  cd "$APP_DIR" || exit 1
  export GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local
  go vet ./... >/dev/null 2>&1
)
if [[ $? -eq 0 ]]; then
  report_pass "go vet ./... clean"
else
  report_fail "go vet ./... clean" "vet returned non-zero"
fi

finalize
