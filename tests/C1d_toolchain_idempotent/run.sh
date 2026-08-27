#!/usr/bin/env bash
# C1d — toolchain installer idempotency / bin_present wiring.
# Builds a scratch prefix whose stub binaries report EXACTLY the pinned
# versions, then asserts the installer treats every tool as installed:
# zero downloads attempted, deterministic output, tree unchanged on rerun.

source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"
# shellcheck source=../../scripts/blocks/02-toolchain.sh
source "$FLEET_ROOT/scripts/blocks/02-toolchain.sh"

scratch="$(mktemp -d)"
prefix="$scratch/prefix"
mkdir -p "$prefix/bin"
trap 'rm -rf "$scratch"' EXIT

stub() { # stub <name> <payload>
  { printf '#!/usr/bin/env bash\n'; printf '%s\n' "$2"; } >"$prefix/bin/$1"
  chmod +x "$prefix/bin/$1"
}

V_KUBESEAL="$(v_strip "$TOOLCHAIN_KUBESEAL_VERSION")"
V_DAGGER="$(v_strip "$TOOLCHAIN_DAGGER_VERSION")"
V_TEMPL="$(v_strip "$TOOLCHAIN_TEMPL_VERSION")"

stub go        "echo 'go version go$TOOLCHAIN_GO_VERSION linux/amd64'"
stub kubectl   "echo '{\"clientVersion\":{\"gitVersion\":\"$TOOLCHAIN_KUBECTL_VERSION\"}}'"
stub restic    "echo 'restic $TOOLCHAIN_RESTIC_VERSION compiled with go'"
stub argocd    "echo 'argocd: $TOOLCHAIN_ARGOCD_VERSION sha-x'"
stub kubeseal  "printf 'kubeseal %s\n' '$TOOLCHAIN_KUBESEAL_VERSION'"
stub dagger    "echo 'dagger $V_DAGGER'"
stub templ     "echo 'templ $V_TEMPL'"
stub tailwindcss "exit 0"

# ── bin_present wiring ──────────────────────────────────────────────────────
assert_rc "bin_present go match"     0 bin_present "$prefix" go     "$TOOLCHAIN_GO_VERSION"
assert_rc "bin_present dagger match" 0 bin_present "$prefix" dagger "$TOOLCHAIN_DAGGER_VERSION"
assert_rc "bin_present version mismatch rejected" 1 \
  bin_present "$prefix" go "9.9.9-not-the-pin"

# ── full skip path: rc=0, no download attempt, every tool skipped ───────────
run1="$(install_toolchain "$prefix" 2>&1)"
rc=$?
if [[ $rc -eq 0 ]]; then
  report_pass "installer rc=0 on fully-pinned prefix"
else
  report_fail "installer rc=0 on fully-pinned prefix" "rc=$rc :: $run1"
  finalize
  exit 1
fi

n_skips="$(grep -c 'already pinned' <<<"$run1")" || true
assert_eq "every tool reported as skip" 8 "$n_skips"

assert_contains "skip line names tool+pin" \
  "[toolchain] go already pinned ($TOOLCHAIN_GO_VERSION)" "$run1"

# ── determinism + tree stability across reruns ──────────────────────────────
run2="$(install_toolchain "$prefix" 2>&1)"
assert_eq "installer output deterministic" "$run1" "$run2"

assert_zero_delta "rerun leaves prefix untouched" "$prefix" \
  install_toolchain "$prefix"

finalize
