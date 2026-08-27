#!/usr/bin/env bash
# C5d — agent protocol integrity (AGENTS.md ↔ ./fleet reality).
# The documented command surface in AGENTS.md must match exactly what the
# control-plane CLI implements (the Go core cmd/fleet behind the shims,
# since WO-4), and the machine-parse summary keywords the doc promises must
# exist verbatim in the implementing source. Static.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"

DOC="$FLEET_ROOT/AGENTS.md"
CLI="$FLEET_ROOT/scripts/fleet"
GO_SRC="$(cat "$FLEET_ROOT"/cmd/fleet/*.go "$FLEET_ROOT"/internal/fleet/*.go 2>/dev/null)"

assert_file "AGENTS.md present" "$DOC"

# commands the doc shows: lines like "./scripts/fleet status" etc.
doc_cmds="$(grep -oE '^\s+\./scripts/fleet [a-z-]+' "$DOC" | awk '{print $2}' | sort -u)"
impl_help="$(bash "$CLI" --help 2>/dev/null)"

missing=0
while IFS= read -r cmd; do
  [[ -z "$cmd" ]] && continue
  printf '%s' "$impl_help" | grep -q "[[:space:]]$cmd" \
    || { report_fail "AGENTS.md documents '$cmd'" "not in ./fleet help"; missing=1; }
done <<<"$doc_cmds"
[[ $missing -eq 0 ]] && report_pass "documented fleet subcommands all implemented"

# inverse: every help command must be documented (registry-check is the
# one alias allowed to stay doc-light)
undoc=0
for hcmd in status doctor approve promote init onboard next wo verify registry-check; do
  grep -qE "^[[:space:]]+\.?/?[[:space:]]*(\./)?(scripts/fleet )?$hcmd" "$DOC" \
    || case "$hcmd" in
         registry-check) : ;; # alias, allowed to be doc-light
         *) report_fail "help exposes undocumented command '$hcmd'" "add to AGENTS.md"; undoc=1 ;;
       esac
done
[[ $undoc -eq 0 ]] && report_pass "help surface fully documented"

# machine contract keywords live in the Go core where they are promised
for kw in "STATUS SUMMARY" "DOCTOR OK" "APPROVED" "VERIFY " "INIT OK" "ONBOARDED" "NEXT action=" "WO LIST"; do
  assert_contains "Go core emits $kw" "$kw" "$GO_SRC"
done
for kw in "PROMOTED component=" "ALREADY AT" "PROMOTE REFUSED"; do
  assert_contains "Go promote emits $kw" "$kw" "$GO_SRC"
done

# the shims stay thin: root pinning + deterministic builder, no logic
assert_contains "scripts/fleet pins FLEET_ROOT" 'FLEET_ROOT=' "$(cat "$CLI")"
assert_contains "scripts/fleet builds via pinned builder" 'ci/build-fleet.sh' "$(cat "$CLI")"
assert_contains "ci/promote.sh delegates to the Go core" 'promote' "$(cat "$FLEET_ROOT/ci/promote.sh")"

# rules section guards the mutation chokepoint & secrets house rule
assert_contains "AGENTS.md locks mutations to ./fleet" \
  "ONLY through ./fleet" "$(cat "$DOC")"
assert_contains "AGENTS.md inherits secrets rule" \
  "NEVER inside" "$(cat "$DOC")"

# rule-7 discipline: the Go core never touches ambient cluster credentials
if printf '%s' "$GO_SRC" | grep -q "KUBECONFIG"; then
  report_fail "Go core free of ambient KUBECONFIG" "KUBECONFIG referenced in cmd/ or internal/"
else
  report_pass "Go core free of ambient KUBECONFIG"
fi

finalize
