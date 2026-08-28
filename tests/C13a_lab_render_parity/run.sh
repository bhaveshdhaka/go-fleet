#!/usr/bin/env bash
# C13a — lab mutation engine parity (WO-8), pure Go.
# The Go renderers/state writers/registry editor must match the FROZEN
# GOLDENS generated once by the authoritative labctl 2.0.0 engine from
# testdata/labfix (sos-lab stays authoritative until WO-8 dual-run
# cutover). No python, no sibling, no network, no cluster at test time:
# the goldens ARE the python oracle, frozen as committed evidence.
# Also guards the editor refusals and the python-format emitters.

source "$FLEET_ROOT/scripts/lib.sh"

# resolve the pinned go toolchain exactly like ci/build-fleet.sh
# shellcheck disable=SC1091
source "$FLEET_ROOT/toolchain.env"
GO_BIN="${FLEET_TOOLCHAIN_PREFIX:-}/bin/go"
[[ -x "$GO_BIN" ]] || GO_BIN="$(command -v go || true)"
if [[ -z "$GO_BIN" ]]; then
  report_skip "go toolchain present" "no go binary on PATH"
  finalize
fi

cd "$FLEET_ROOT"

# every parity test must exist and actually run (no SKIP, no silent drift)
GOLDEN_DIR="$FLEET_ROOT/internal/fleet/testdata/golden"
for g in services kaniko_single kaniko_overlay monitor tunnel_ingress; do
  assert_file "golden present: $g" "$GOLDEN_DIR/$g.json"
done
for s in deploy-with-sha deploy-no-sha build-null-sha rollback; do
  found=""
  for f in "$GOLDEN_DIR/state/$s"/*.json; do
    [[ -s "$f" ]] && found=yes
  done
  if [[ "$found" == yes ]]; then
    report_pass "state golden present: $s"
  else
    report_fail "state golden present: $s" "no golden file under $GOLDEN_DIR/state/$s"
  fi
done

out="$(env GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local CGO_ENABLED=0 \
  "$GO_BIN" test ./internal/fleet -count=1 -v \
  -run 'TestLabRenderParity|TestLabStateParity|TestGatusEmitter|TestRegistryEdits|TestLabEnvOrder' 2>&1)"
rc=$?
if [[ $rc -ne 0 ]]; then
  report_fail "parity suite passes" "go test rc=$rc"
  printf '%s\n' "$out" | tail -20
  finalize
fi
report_pass "parity suite passes"

for t in TestLabRenderParity TestLabStateParity TestGatusEmitter TestRegistryEdits TestLabEnvOrder; do
  if printf '%s\n' "$out" | grep -q -- "--- PASS: $t"; then
    report_pass "parity ran: $t"
  elif printf '%s\n' "$out" | grep -q -- "--- SKIP: $t"; then
    report_fail "parity ran: $t" "test skipped — parity must never skip silently"
  else
    report_fail "parity ran: $t" "no result line in go test output"
  fi
done

# the rule-7 guard must still hold with the new engine files on board
GO_SRC="$(cat "$FLEET_ROOT"/internal/fleet/*.go "$FLEET_ROOT"/cmd/fleet/*.go 2>/dev/null)"
if printf '%s' "$GO_SRC" | grep -q "os\.Environ()"; then
  report_fail "engine keeps rule-7 discipline" "os.Environ() found in Go core"
else
  report_pass "engine keeps rule-7 discipline"
fi

finalize
