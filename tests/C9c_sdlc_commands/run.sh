#!/usr/bin/env bash
# C9c — SDLC command surface (init/onboard/next/wo/verify), WO-4 minimal.
# Fresh scaffold from embedded templates must reach doctor ALL CLEAR after
# onboarding a real component; guidance (next) must point at the first
# legal action; verify must journal its measured result. Everything happens
# in scratch; the live repo is never touched.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

proj="$scratch/proj"
o="$(FLEET_ROOT= "$F" init "$proj" 2>&1)"
{ [[ "$o" == "INIT OK dir=$proj files=6"* ]]; } \
  && report_pass "init scaffolds 6 contract files" || report_fail "init scaffolds 6 contract files" "$o"

tree1="$(cd "$proj" && find . -type f | sort | xargs sha256sum | sha256sum)"
o="$(FLEET_ROOT= "$F" init "$proj" 2>&1)"
tree2="$(cd "$proj" && find . -type f | sort | xargs sha256sum | sha256sum)"
{ [[ "$o" == INIT\ ALREADY* && "$tree1" == "$tree2" ]]; } \
  && report_pass "init repeat idempotent (zero-delta)" || report_fail "init repeat idempotent (zero-delta)" "$o"

o="$(FLEET_ROOT="$proj" "$F" onboard demo --kind=cli --description=smoke 2>&1)"
assert_contains "onboard contract line" "ONBOARDED component=demo registry=+1 pipeline=ci/pipelines/demo.yaml state=+1" "$o"
assert_file "pipeline file created" "$proj/ci/pipelines/demo.yaml"

o="$(FLEET_ROOT="$proj" "$F" doctor 2>&1)"
assert_contains "doctor reports missing app dir (honest drift)" "DOCTOR FAIL" "$o"

mkdir -p "$proj/apps/demo" "$proj/infra/k8s"
printf 'package main\nfunc main() {}\n' > "$proj/apps/demo/main.go"
o="$(FLEET_ROOT="$proj" "$F" doctor 2>&1)"
assert_contains "doctor ALL CLEAR after materializing" "DOCTOR OK checked_components=1 issues=0" "$o"

o="$(FLEET_ROOT="$proj" "$F" onboard demo 2>&1)"; rc=$?
{ [[ $rc -eq 1 && "$o" == *"already registered"* ]]; } \
  && report_pass "duplicate onboard refused" || report_fail "duplicate onboard refused" "$rc :: $o"
o="$(FLEET_ROOT="$proj" "$F" onboard BAD_NAME 2>&1)"; rc=$?
[[ $rc -eq 1 ]] && report_pass "invalid name refused" || report_fail "invalid name refused" "$rc :: $o"

o="$(FLEET_ROOT="$proj" "$F" status)"
assert_contains "status shows onboarded component" "STATUS component=demo kind=cli stage=built" "$o"
o="$(FLEET_ROOT="$proj" "$F" next 2>&1)"
assert_contains "next suggests first legal hop" "NEXT action=./scripts/fleet promote demo dev" "$o"

o="$(FLEET_ROOT="$proj" "$F" wo new WO-901 "smoke workorder" 2>&1)"
assert_contains "wo new contract line" "WO NEW id=WO-901" "$o"
o="$(FLEET_ROOT="$proj" "$F" wo list 2>&1)"
assert_contains "wo list shows OPEN workorder" "WO id=WO-901 status=OPEN" "$o"

# verify: journals the measured result of a corpus subset in a repo COPY
repo="$scratch/repo"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$repo" -xf -
j_before=$(grep -c '^# verify' "$repo/lifecycle/journal/events.log" || true)
o="$(FLEET_ROOT="$repo" "$F" verify C1a_toolchain_env 2>&1)"; rc=$?
{ [[ $rc -eq 0 && "$o" == "VERIFY units=1 pass=24 fail=0 skip=0 result=OK" ]]; } \
  && report_pass "verify machine summary" || report_fail "verify machine summary" "$rc :: $o"
j_after=$(grep -c '^# verify' "$repo/lifecycle/journal/events.log" || true)
assert_eq "verify journaled its result" $((j_before + 1)) "$j_after"

finalize
