#!/usr/bin/env bash
# C1c — toolchain source resolution.
# Asserts every pinned tool resolves to a non-empty, well-formed download
# URL containing its exact pinned version. Deterministic: pure string
# construction, no network, no mutation.

source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../../scripts/blocks/02-toolchain.sh
source "$FLEET_ROOT/scripts/blocks/02-toolchain.sh"

check_resolution() {
  local tool=$1
  local url v
  url="$(tool_source "$tool")" || { report_fail "$tool resolves" "tool_source failed"; return; }
  v="$(pin "$tool")"
  assert_contains "$tool URL http" "http" "$url"
  # every source must embed its own pinned version
  assert_contains "$tool URL embeds pin" "$v" "$url"
}

for tool in go kubectl restic argocd kubeseal templ tailwindcss; do
  check_resolution "$tool"
done

# dagger is an installer script (no version in URL) — still must resolve to http
url="$(tool_source dagger)"
assert_contains "dagger URL http" "http" "$url"

finalize
