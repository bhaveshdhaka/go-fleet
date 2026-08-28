#!/usr/bin/env bash
# C15a — arjun-hk site contract (WO-10), hermetic: builds the site with
# block03, runs the BINARY on a loopback port and asserts the iOS /
# Safari / retina / dollarbucks-menu contract against the SERVED page.
# No cluster, no external network (loopback only).

source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"
# shellcheck source=../scripts/blocks/03-pipeline.sh
source "$FLEET_ROOT/scripts/blocks/03-pipeline.sh"

if ! command -v go >/dev/null 2>&1; then
  report_skip "go toolchain present" "no go binary on PATH"
  finalize
fi

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"; if [[ -n ${srv_pid:-} ]]; then kill "$srv_pid" 2>/dev/null; fi; wait 2>/dev/null' EXIT
build_app arjun-hk "$scratch" >/dev/null 2>&1 \
  || { report_fail "site builds via block03" "build_app failed"; finalize; }
report_pass "site builds via block03"

PORT=18642
PORT="$PORT" "$scratch/arjun-hk" & srv_pid=$!
up=""
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
  curl -sf --max-time 2 "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && { up=yes; break; }
  sleep 0.3
done
[[ $up == yes ]] || { report_fail "server comes up" "healthz not ready"; finalize; }
report_pass "server comes up (loopback)"

page="$(curl -sf --max-time 5 "http://127.0.0.1:$PORT/")"
healthz="$(curl -sf --max-time 5 "http://127.0.0.1:$PORT/healthz")"

# healthz for the k8s probe
assert_eq "healthz returns ok" "ok" "$healthz"

# iOS contract
assert_contains "iOS: viewport-fit=cover" 'viewport-fit=cover' "$page"
assert_contains "iOS: apple web-app capable" 'apple-mobile-web-app-capable' "$page"
assert_contains "iOS: translucent status bar" 'apple-mobile-web-app-status-bar-style' "$page"
assert_contains "iOS: theme-color" 'name="theme-color"' "$page"
assert_contains "iOS: safe-area insets" 'env(safe-area-inset-top)' "$page"

# Safari contract
assert_contains "Safari: system font stack (-apple-system)" '-apple-system' "$page"
assert_contains "Safari: font smoothing" '-webkit-font-smoothing' "$page"
if printf '%s' "$page" | grep -qE 'https?://[a-z0-9.-]+/|url\(http|<script|@import|integrity=|fonts\.googleapis'; then
  report_fail "Safari: zero external fetches/scripts" "external reference found"
else
  report_pass "Safari: zero external fetches/scripts"
fi

# retina contract
assert_contains "retina: inline SVG artwork" '<svg' "$page"
assert_contains "retina: 2x media refinement" 'min-device-pixel-ratio: 2' "$page"
if printf '%s' "$page" | grep -qE '<img[[:space:]]'; then
  report_fail "retina: no raster <img> tags (SVG only)" "raster image found"
else
  report_pass "retina: no raster <img> tags (SVG only)"
fi

# the dollarbucks menu itself
for item in "Waffles" "Dino nuggets" "Mango smoothie" "Sleep-in Saturday voucher"; do
  assert_contains "menu item: $item" "$item" "$page"
done
assert_contains "menu currency note" "prices in dollarbucks" "$page"

finalize
