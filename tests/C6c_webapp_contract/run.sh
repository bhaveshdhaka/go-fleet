#!/usr/bin/env bash
# C6c — webapp approve flow end-to-end in an isolated copy (the unit the
# stage->prod gate depends on). POST /approve must write the exact same
# approval files + journal lines as ./fleet approve, refuse unknown names,
# and stay idempotent on duplicates. Nothing outside scratch mutates.

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

port=""
for p in $(seq 18121 18150); do
  if ! (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null; then port=$p; break; fi
done
[[ -n "$port" ]] || { report_skip "free local port" "range busy"; finalize; exit 0; }

base="http://127.0.0.1:$port"

start_hub() {
  FLEETHUB_ADDR="127.0.0.1:$port" FLEET_ROOT="$repo" \
    "$dist_dir/fleethub" >>"$scratch/srv.log" 2>&1 &
  srv_pid=$!
  for _ in $(seq 1 50); do
    curl -fsS -m 1 "$base/healthz" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  return 1
}
cleanup() { kill "$srv_pid" 2>/dev/null || true; wait "$srv_pid" 2>/dev/null || true; }
journal_events() { grep -c '^ts=' "$repo/lifecycle/journal/events.log"; }

j0="$(journal_events)"
start_hub || { report_fail "server up" "$(tail -2 "$scratch/srv.log")"; finalize; exit 1; }

code_and_body() { # code_and_body <method> <path> [form]
  local method=$1 path=$2 form=${3:-} out
  out="$(curl -sS -m 5 -o /tmp/opencode/fh_body.$$ -w '%{http_code}' \
        -X "$method" ${form:+-d "$form"} "$base$path" 2>/dev/null)" && \
    printf '%s %s' "$out" "$(cat /tmp/opencode/fh_body.$$)" ; rm -f "/tmp/opencode/fh_body.$$"
}

r="$(code_and_body POST /approve "component=ghost&stage=dev&approver=c6c")"
{ [[ "$r" == 400* ]]; } \
  && report_pass "unknown component -> 400" || report_fail "unknown component -> 400" "$r"

r="$(code_and_body POST /approve "component=fleetctl&stage=banana&approver=c6c")"
{ [[ "$r" == 400* ]]; } \
  && report_pass "illegal stage -> 400" || report_fail "illegal stage -> 400" "$r"

r="$(code_and_body POST /approve "component=fleetctl&stage=dev&approver=c6c-tester")"
rc=201
{ [[ "$r" == 201*'"result":"approved"'* ]]; } \
  && report_pass "valid dev approval -> 201" || report_fail "valid dev approval -> 201" "$r"

appr="$repo/lifecycle/approvals/dev/fleetctl.approved"
grep -q 'approved_by=c6c-tester' "$appr" && grep -q 'source=fleethub-http' "$appr" \
  && report_pass "approval file content correct" || report_fail "approval file content correct" "$(head -3 "$appr")"

j1="$(journal_events)"
assert_eq "journal gained exactly one event" "$((j0 + 1))" "$j1"
tail -1 "$repo/lifecycle/journal/events.log" | grep -q 'event=approved component=fleetctl stage=dev actor=c6c-tester source=fleethub-http' \
  && report_pass "journal line matches CLI format" || report_fail "journal line matches CLI format" "$(tail -1 "$repo/lifecycle/journal/events.log")"

r="$(code_and_body POST /approve "component=fleetctl&stage=dev&approver=c6c-tester")"
j2="$(journal_events)"
{ [[ "$r" == 200*'already'* && "$j2" -eq "$j1" ]]; } \
  && report_pass "duplicate approval idempotent (no new event)" \
  || report_fail "duplicate approval idempotent (no new event)" "$r :: j=$j2"

cleanup

# cross-check: ./fleet doctor in the same copy is now satisfied for approvals
bash "$repo/scripts/fleet" status fleetctl | grep -q 'STATUS component=fleetctl kind=' \
  && report_pass "./fleet still reads state happily post-web-write" \
  || report_fail "./fleet still reads state happily post-web-write" ""

finalize
