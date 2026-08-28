#!/usr/bin/env bash
# fleet shared test library.
# Sources by every test unit. Provides deterministic assertion helpers that
# emit machine-readable [PASS|FAIL|SKIP] lines and exit non-zero on failure.

set -uo pipefail

export FLEET_ROOT
FLEET_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# ---------------------------------------------------------------------------
# Result line format (single source of truth for the report):
#   [PASS] <chunk> <name>
#   [FAIL] <chunk> <name> :: <reason>
#   [SKIP] <chunk> <name> :: <reason>
# ---------------------------------------------------------------------------

PASS=0
FAIL=0
SKIP=0

report_pass() {
  printf '[PASS] %s %s\n' "$CHUNK" "$1"
  PASS=$((PASS + 1))
}

report_fail() {
  printf '[FAIL] %s %s :: %s\n' "$CHUNK" "$1" "${2:-no reason}"
  FAIL=$((FAIL + 1))
}

report_skip() {
  printf '[SKIP] %s %s :: %s\n' "$CHUNK" "$1" "${2:-not applicable}"
  SKIP=$((SKIP + 1))
}

# assert_eq <name> <expected> <actual>
assert_eq() {
  local name=$1 expected=$2 actual=$3
  if [[ "$expected" == "$actual" ]]; then
    report_pass "$name"
  else
    report_fail "$name" "expected '$expected' got '$actual'"
  fi
}

# assert_rc <name> <expected_rc> <cmd...>
assert_rc() {
  local name=$1 expected=$2
  shift 2
  "$@" >/dev/null 2>&1
  local rc=$?
  if [[ $rc -eq $expected ]]; then
    report_pass "$name"
  else
    report_fail "$name" "expected rc=$expected got rc=$rc"
  fi
}

# assert_contains <name> <needle> <haystack>
assert_contains() {
  local name=$1 needle=$2 haystack=$3
  if [[ "$haystack" == *"$needle"* ]]; then
    report_pass "$name"
  else
    report_fail "$name" "missing '$needle'"
  fi
}

# assert_empty <name> <value>
assert_empty() {
  local name=$1 value=$2
  if [[ -z "$value" ]]; then
    report_pass "$name"
  else
    report_fail "$name" "expected empty, got '$value'"
  fi
}

# assert_file <name> <path>
assert_file() {
  local name=$1 path=$2
  if [[ -f "$path" ]]; then
    report_pass "$name"
  else
    report_fail "$name" "missing file '$path'"
  fi
}

# assert_not_file <name> <path>
assert_not_file() {
  local name=$1 path=$2
  if [[ ! -e "$path" ]]; then
    report_pass "$name"
  else
    report_fail "$name" "file should not exist '$path'"
  fi
}

# assert_zero_delta <name> <dir> <cmd...>
# Runs cmd twice over dir, asserting byte-identical tree hash (idempotency).
assert_zero_delta() {
  local name=$1 dir=$2
  shift 2
  "$@" >/dev/null 2>&1
  local h1
  h1="$(tree_hash "$dir")"
  "$@" >/dev/null 2>&1
  local h2
  h2="$(tree_hash "$dir")"
  if [[ "$h1" == "$h2" ]]; then
    report_pass "$name"
  else
    report_fail "$name" "second run changed tree ($h1 != $h2)"
  fi
}

# tree_hash <dir> : deterministic hash of a tree's paths+contents (sorted).
tree_hash() {
  local dir=$1
  (
    cd "$dir" 2>/dev/null || { printf 'MISSING'; return; }
    find . -type f -print0 | sort -z | xargs -0 -r sha256sum
  ) | sha256sum | cut -d' ' -f1
}

# jsonq_build <out> : build tests/lib/jsonq.go — stdlib-only JSON query
# helper. The corpus validates its --json fixtures with this; the test
# workflow carries no interpreted runtimes (WO-20 close-out).
jsonq_build() {
  local out=$1
  [[ -x "$out" ]] && return 0
  # shellcheck disable=SC1091
  source "$FLEET_ROOT/toolchain.env"
  local go_bin="${FLEET_TOOLCHAIN_PREFIX:-}/bin/go"
  if [[ ! -x "$go_bin" ]]; then
    go_bin="$(command -v go)" \
      || { echo "ERROR: no usable go binary (expected toolchain prefix or PATH)" >&2; return 1; }
  fi
  (cd "$FLEET_ROOT" || return 1
   GOPROXY=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local CGO_ENABLED=0 \
     "$go_bin" build -trimpath -o "$out" tests/lib/jsonq.go) || return 1
}

# finalize : emit aggregate (to ${RESULT_DIR}/report.txt) and overall status.
# Reads RESULT_DIR from the environment. Run units must export RESULT_DIR.
finalize() {
  local result_dir="${RESULT_DIR:-.}"
  mkdir -p "$result_dir"
  printf 'AGG %s PASS=%d FAIL=%d SKIP=%d\n' "$CHUNK" "$PASS" "$FAIL" "$SKIP" \
    >> "$result_dir/report.txt"
  if [[ $FAIL -gt 0 ]]; then
    return 1
  fi
  return 0
}
