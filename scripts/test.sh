#!/usr/bin/env bash
# fleet test harness (C0).
#
# Discovers test units under $FLEET_ROOT/tests, resolves their dependency
# DAG, topologically sorts, then runs each unit in order. If a unit's
# dependency fails, that unit is SKIPped (never a false green). Output is a
# deterministic, sorted report. Mutating units are asserted idempotent.
#
# Usage:
#   scripts/test.sh                 run the full corpus
#   scripts/test.sh <unit>...       run only listed units (+ their deps)
#   scripts/test.sh --self          harness self-test
#   scripts/test.sh --list          list units in run order

set -uo pipefail
FLEET_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TESTS_DIR="$FLEET_ROOT/tests"

self=false
list_only=false
declare -a requested=()

for arg in "$@"; do
  case "$arg" in
    --self) self=true ;;
    --list) list_only=true ;;
    *) requested+=("$arg") ;;
  esac
done

# ---------------------------------------------------------------------------
# Self-test of the harness itself (C0). Runs before any real unit.
# ---------------------------------------------------------------------------
self_test() {
  local tmp
  tmp="$(mktemp -d)"
  printf 'C0 self: %s\n' "$tmp"
  # 1. lib.sh sources cleanly
  bash -n "$FLEET_ROOT/scripts/lib.sh" || { echo "lib.sh syntax error"; rm -rf "$tmp"; return 1; }
  # 2. tree_hash is deterministic
  mkdir -p "$tmp/a/sub"
  printf 'x\n' > "$tmp/a/1.txt"
  printf 'y\n' > "$tmp/a/sub/2.txt"
  local h1 h2
  h1="$(tree_hash "$tmp/a")"
  h2="$(tree_hash "$tmp/a")"
  [[ "$h1" == "$h2" ]] || { echo "tree_hash not deterministic"; rm -rf "$tmp"; return 1; }
  printf 'PASS C0 harness self-test\n'
  rm -rf "$tmp"
}

# ---------------------------------------------------------------------------
# Discover units.
# ---------------------------------------------------------------------------
declare -a units=()
discover() {
  local f
  for f in "$TESTS_DIR"/*/run.sh; do
    [[ -e "$f" ]] || continue
    units+=("$(basename "$(dirname "$f")")")
  done
}

# unit_deps <unit> -> prints dependency unit names, one per line
unit_deps() {
  local deps_file="$TESTS_DIR/$1/DEPS"
  if [[ -f "$deps_file" ]]; then
    grep -v '^[[:space:]]*#' "$deps_file" | sed '/^[[:space:]]*$/d'
  fi
}

# unit_has_dep <unit> <dep>
unit_has_dep() {
  local d
  for d in $(unit_deps "$1"); do
    [[ "$d" == "$2" ]] && return 0
  done
  return 1
}

# ---------------------------------------------------------------------------
# Topological sort (Kahn). Unknown/self/missing dependencies are errors.
# ---------------------------------------------------------------------------
declare -a order=()
topo_sort() {
  local remaining=("${units[@]}")
  local guard=0
  while [[ ${#remaining[@]} -gt 0 ]]; do
    guard=$((guard + 1))
    [[ $guard -gt 1000 ]] && { echo "FATAL: dependency cycle"; return 1; }
    local progressed=false
    local new_remaining=()
    local u
    for u in "${remaining[@]}"; do
      local ready=true
      local d
      for d in $(unit_deps "$u"); do
        if [[ "$d" == "$u" ]]; then
          echo "FATAL: $u depends on itself"
          return 1
        fi
        if ! contains "$d" "${units[@]}"; then
          echo "FATAL: $u depends on unknown unit '$d'"
          return 1
        fi
        if ! contains "$d" "${order[@]}"; then
          ready=false
          break
        fi
      done
      if $ready; then
        order+=("$u")
        progressed=true
      else
        new_remaining+=("$u")
      fi
    done
    if ! $progressed; then
      echo "FATAL: dependency cycle among: ${new_remaining[*]}"
      return 1
    fi
    remaining=("${new_remaining[@]}")
  done
}

contains() {
  local needle=$1
  shift
  local x
  for x in "$@"; do
    [[ "$x" == "$needle" ]] && return 0
  done
  return 1
}

# ---------------------------------------------------------------------------
# Run a single unit in a scratch dir, capturing its report.
# ---------------------------------------------------------------------------
run_unit() {
  local unit=$1 result_dir=$2
  local unit_dir="$TESTS_DIR/$unit"
  (
    cd "$unit_dir" || exit 3
    export CHUNK="$unit" FLEET_ROOT RESULT_DIR="$result_dir"
    # shellcheck disable=SC1091
    source "$FLEET_ROOT/scripts/lib.sh"
    bash run.sh
    local rc=$?
    finalize || rc=1
    exit $rc
  )
  return $?
}

main() {
  source "$FLEET_ROOT/scripts/lib.sh"

  if $self; then
    self_test || return 1
    [[ ${#requested[@]} -eq 0 ]] && return 0
  fi

  discover
  if ! topo_sort; then
    return 1
  fi

  if $list_only; then
    for u in "${order[@]}"; do
      echo "$u"
    done
    return 0
  fi

  # Restrict to requested units (+ their transitive deps) when given.
  if [[ ${#requested[@]} -gt 0 ]]; then
    local filtered=()
    local u
    for u in "${order[@]}"; do
      if contains "$u" "${requested[@]}"; then
        filtered+=("$u")
      fi
    done
    order=("${filtered[@]}")
  fi

  local result_dir
  result_dir="$(mktemp -d)"
  local failed=false
  local inline_skip=0

  for u in "${order[@]}"; do
    if $failed; then
      printf '[SKIP] %s (dependency failed)\n' "$u"
      inline_skip=$((inline_skip + 1))
      continue
    fi
    echo "== $u"
    if run_unit "$u" "$result_dir"; then
      :
    else
      failed=true
    fi
  done

  local total_pass total_fail total_skip
  total_pass="$(awk -F'[ =]+' '/^AGG/{s+=$4}END{print s+0}' "$result_dir"/report.txt 2>/dev/null || echo 0)"
  total_fail="$(awk -F'[ =]+' '/^AGG/{s+=$6}END{print s+0}' "$result_dir"/report.txt 2>/dev/null || echo 0)"
  total_skip="$(awk -F'[ =]+' '/^AGG/{s+=$8}END{print s+0}' "$result_dir"/report.txt 2>/dev/null || echo 0)"
  total_skip=$((total_skip + inline_skip))

  echo
  echo "FLEET SUMMARY  units_run=${#order[@]} pass=$total_pass fail=$total_fail skip=$total_skip"
  rm -rf "$result_dir"
  if $failed; then
    return 1
  fi
  return 0
}

main "$@"
