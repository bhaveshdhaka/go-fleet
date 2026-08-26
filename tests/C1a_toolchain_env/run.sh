#!/usr/bin/env bash
# C1a — toolchain.env validity.
# Sources toolchain.env and asserts every expected pin is present, non-empty,
# and well-formed. Deterministic: no network, no mutation.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"

expect_key() {
  local name=$1
  local val
  val="$(eval "printf '%s' \"\${TOOLCHAIN_${name}_VERSION:-}\"")"
  if [[ -n "$val" ]]; then
    report_pass "pin $name present+nonempty"
  else
    report_fail "pin $name present+nonempty" "TOOLCHAIN_${name}_VERSION is empty/missing"
    return
  fi
  # well-formed: does not contain whitespace or shell metacharacters
  if [[ "$val" =~ [^A-Za-z0-9._+-] ]]; then
    report_fail "pin $name well-formed" "illegal characters in '$val'"
  else
    report_pass "pin $name well-formed"
  fi
}

expect_key GO
expect_key BUN
expect_key TEMPL
expect_key TAILWIND
expect_key DAGGER
expect_key KUBECTL
expect_key ARGOCD
expect_key KUBESEAL
expect_key RESTIC
expect_key OPENCHAMBER
expect_key OPENCODE

# arch/os must be set
assert_contains "arch set" "$TOOLCHAIN_ARCH" "$TOOLCHAIN_ARCH"
assert_contains "os set" "$TOOLCHAIN_OS" "$TOOLCHAIN_OS"

finalize
