#!/usr/bin/env bash
# C6b — fleethub serving contract against an ISOLATED repo copy.
# Boots the freshly built binary pointed at a throwaway copy of ops/ and
# lifecycle/, then asserts read endpoints expose exactly on-disk truth.
# Server writes nothing during this unit (no POSTs here).

source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"

scratch="$(mktemp -d)"
repo="$scratch/fleet"
dist_dir="$scratch/dist"
mkdir -p "$repo" "$dist_dir"
trap 'rm -rf "$scratch"; [[ -n ${srv_pid:-} ]] && kill "$srv_pid" 2>/dev/null' EXIT
tar -C "$FLEET_ROOT" --exclude=.git -cf - . | tar -C "$repo" -xf -

if ! command -v go >/dev/null 2>&1; then
  report_skip "go available" "toolchain missing"
  finalize; exit 0
fi
# shellcheck source=scripts/blocks/03-pipeline.sh
source "$FLEET_ROOT/scripts/blocks/03-pipeline.sh"

build_app fleethub "$dist_dir" >/dev/null 2>&1 \
  || { report_fail "pre-build" "block03 build failed"; finalize; exit 1; }
report_pass "fleethub built in copy"

port=""
for p in $(seq 18091 18120); do
  if ! (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null; then port=$p; break; fi
done
[[ -n "$port" ]] || { report_skip "free local port" "range busy"; finalize; exit 0; }

FLEETHUB_ADDR="127.0.0.1:$port" FLEET_ROOT="$repo" \
  "$dist_dir/fleethub" >"$scratch/srv.log" 2>&1 &
srv_pid=$!

up=0
for _ in $(seq 1 50); do
  if curl -fsS -m 1 "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then up=1; break; fi
  sleep 0.2
done

cleanup() { kill "$srv_pid" 2>/dev/null || true; wait "$srv_pid" 2>/dev/null || true; }

if [[ $up -eq 0 ]]; then
  report_fail "server boots + healthz" "$(tail -2 "$scratch/srv.log")"
  cleanup
  finalize; exit 1
fi
report_pass "server boots + healthz"

api="$(curl -fsS -m 5 "http://127.0.0.1:$port/api/projects" 2>&1)"
rc=$?
{ [[ $rc -eq 0 && "$api" == *'"name":"fleetctl"'* && "$api" == *'"name":"fleethub"'* ]]; } \
  && report_pass "api lists both components" || report_fail "api lists both components" "$rc :: $api"

apicount="$(printf '%s' "$api" | grep -o '"stage":"' | wc -l | tr -d ' ')"
n_comp="$(grep -c '^  - name: ' "$FLEET_ROOT/ops/PROJECTS.yaml")"
assert_eq "api carries stage field per component" "$n_comp" "$apicount"

page="$(curl -fsS -m 5 "http://127.0.0.1:$port/" 2>&1)"
grep -q fleetctl <<<"$page" && grep -q 'fleet hub' <<<"$page" \
  && report_pass "dashboard html renders registry" \
  || report_fail "dashboard html renders registry" "${page:0:120}"

cleanup
finalize
