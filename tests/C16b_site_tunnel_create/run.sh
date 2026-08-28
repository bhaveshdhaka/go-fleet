#!/usr/bin/env bash
# C16b — site tunnel create (WO-15 piece 2), hermetic via the Go httptest
# Cloudflare double: the exact CF call shapes (accounts, tunnel create
# with config_src=cloudflare, zone lookup), tunnel token stored 0600 in
# the secrets home, registry revalidates, journal never carries tokens.

source "$FLEET_ROOT/scripts/lib.sh"

# resolve the pinned go toolchain exactly like ci/build-fleet.sh
source "$FLEET_ROOT/toolchain.env"
GO_BIN="${FLEET_TOOLCHAIN_PREFIX:-}/bin/go"
[[ -x "$GO_BIN" ]] || GO_BIN="$(command -v go || true)"
if [[ -z "$GO_BIN" ]]; then
  report_skip "go toolchain present" "no go binary on PATH"
  finalize
fi

cd "$FLEET_ROOT"
out="$(env GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local CGO_ENABLED=0 \
  "$GO_BIN" test ./internal/fleet -count=1 -v \
  -run 'TestSiteTunnelCreate' 2>&1)"
rc=$?
assert_eq "tunnel create go test rc" "0" "$rc"
assert_contains "tunnel create asserts ran" "PASS: TestSiteTunnelCreate" "$out"

finalize
