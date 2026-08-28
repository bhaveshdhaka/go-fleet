#!/usr/bin/env bash
# C15b — arjun-hk SDLC wiring (WO-10): onboarded component registry entry,
# pipeline file, state entry, pinned module, and vet-clean build. Static +
# offline.

source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"

APP_DIR="$FLEET_ROOT/apps/arjun-hk"

assert_file "go.mod pinned module" "$APP_DIR/go.mod"
got="$(awk 'tolower($1)=="go"{print $2}' "$APP_DIR/go.mod")"
assert_eq "go.mod pins TOOLCHAIN_GO_VERSION" "$TOOLCHAIN_GO_VERSION" "$got"
assert_file "page.html present" "$APP_DIR/page.html"
assert_file "container Dockerfile present" "$APP_DIR/Dockerfile"

reg_block="$(sed -n '/  - name: arjun-hk/,/enabled: true/p' "$FLEET_ROOT/ops/PROJECTS.yaml")"
printf '%s\n' "$reg_block" | grep -q "kind: service" && report_pass "registry: kind service" || report_fail "registry: kind service" "missing"
printf '%s\n' "$reg_block" | grep -q "port: 8080" && report_pass "registry: port 8080" || report_fail "registry: port 8080" "missing"
printf '%s\n' "$reg_block" | grep -q "pipeline: ci/pipelines/arjun-hk.yaml" && report_pass "registry: pipeline wired" || report_fail "registry: pipeline wired" "missing"

assert_file "pipeline file exists" "$FLEET_ROOT/ci/pipelines/arjun-hk.yaml"
grep -q "component: arjun-hk" "$FLEET_ROOT/ci/pipelines/arjun-hk.yaml" \
  && report_pass "pipeline: component name" || report_fail "pipeline: component name" "missing"
grep -A3 "prod:" "$FLEET_ROOT/ci/pipelines/arjun-hk.yaml" | grep -q "cluster-apply" \
  && report_pass "pipeline: prod mode cluster-apply" || report_fail "pipeline: prod mode cluster-apply" "missing"

FLEET_ROOT="$FLEET_ROOT" "$FLEET_ROOT/scripts/fleet" status arjun-hk | grep -q "arjun-hk" \
  && report_pass "fleet status shows arjun-hk" || report_fail "fleet status shows arjun-hk" "missing"

(
  cd "$APP_DIR" || exit 1
  export GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local
  go vet ./... >/dev/null 2>&1
)
[[ $? -eq 0 ]] && report_pass "go vet clean" || report_fail "go vet clean" "vet non-zero"

finalize
