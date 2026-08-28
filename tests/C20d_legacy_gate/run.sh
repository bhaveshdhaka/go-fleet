#!/usr/bin/env bash
# C20d — legacy gate (WO-20 piece 4; close-out hardened): no interpreted
# runtimes anywhere — repo tree (go/yaml/md/sh), fleet-rendered goldens,
# site templates, and the LIVE fleet-managed workloads. Third-party
# upstream images (the owner's chosen apps) are inventoried, not failed.
# The scans run from $FLEET_ROOT (the corpus runs units with cwd=unit dir)
# and cover tests/ + testdata/; excluded are only this gate's own pattern
# literals, vendored/.vm/dist trees, and historical workorder prose.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
cd "$FLEET_ROOT" || exit 1
fail=0

# 1. no interpreted-runtime image refs or invocations in fleet-owned text
while IFS= read -r f; do
  case "$f" in
    ./tests/C20d_legacy_gate/*) continue ;;
    # this gate's own regex literals; workorders: historical prose
  esac
  case "$f" in
    *.yaml|*.yml|*.json|*Dockerfile*) pat="python:[0-9]|node:[0-9]+-alpine|ruby:[0-9]" ;;
    *.sh|*.go) pat="python3? +-m |python3? +/|\"python3?\"|'python3?'" ;;
    *) continue ;;
  esac
  if grep -qn -E "$pat" "$f" 2>/dev/null; then
    echo "FAIL-LEGACY $f: interpreted runtime"
    fail=1
  fi
done < <(find . -type f \( -name "*.go" -o -name "*.yaml" -o -name "*.yml" -o -name "*.json" -o -name "*.sh" -o -name "*.md" -o -name "Dockerfile*" \) \
  ! -path "./.git/*" ! -path "./.toolchain/*" ! -path "./.vm/*" ! -path "./dist/*" ! -path "./workorders/*")

[[ $fail -eq 0 ]] && report_pass "no interpreted runtimes in fleet-owned tree (repo-wide)" \
  || report_fail "no interpreted runtimes in fleet-owned tree (repo-wide)" "see FAIL-LEGACY above"

# 2. no .py files anywhere in the repo
pyc=$(find . -name "*.py" ! -path "./.git/*" ! -path "./.toolchain/*" ! -path "./.vm/*" ! -path "./dist/*" | wc -l)
[[ "$pyc" -eq 0 ]] && report_pass "no .py files in fleet tree (repo-wide)" \
  || report_fail "no .py files in fleet tree (repo-wide)" "$pyc found"

# 3. rendered specs: the authoritative fleet output stays renderer-free —
# the golden must never render the retired interpreted renderer again
if grep -q "python:3.12-alpine" internal/fleet/testdata/golden/monitor.json 2>/dev/null; then
  echo "FAIL-LEGACY golden/monitor.json still renders the retired renderer"
  fail=1
fi
[[ $fail -eq 0 ]] && report_pass "rendered specs renderer-free (golden flipped)" \
  || report_fail "rendered specs renderer-free (golden flipped)" "monitor.json"

finalize
