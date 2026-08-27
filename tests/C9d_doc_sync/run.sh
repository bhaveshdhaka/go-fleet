#!/usr/bin/env bash
# C9d — Go core documentation sync + rule-7 discipline (WO-4).
# Every command the binary offers is documented in AGENTS.md, every
# documented command exists, the machine contract keywords live in the Go
# source, the shims stay thin and root-pinned, and the Go core never reads
# ambient KUBECONFIG (AGENTS.md rule 7).

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"

DOC="$FLEET_ROOT/AGENTS.md"
GO_SRC="$(cat "$FLEET_ROOT"/cmd/fleet/*.go "$FLEET_ROOT"/internal/fleet/*.go 2>/dev/null)"
SHIM="$(cat "$FLEET_ROOT/scripts/fleet")"

assert_file "AGENTS.md present" "$DOC"

impl_help="$(bash "$FLEET_ROOT/scripts/fleet" --help 2>/dev/null)"
doc_cmds="$(grep -oE '^\s+\./scripts/fleet [a-z-]+' "$DOC" | awk '{print $2}' | sort -u)"

missing=0
while IFS= read -r cmd; do
  [[ -z "$cmd" ]] && continue
  printf '%s' "$impl_help" | grep -q "[[:space:]]$cmd" \
    || { report_fail "doc command '$cmd' implemented" "not in help"; missing=1; }
done <<<"$doc_cmds"
[[ $missing -eq 0 ]] && report_pass "all documented commands implemented"

undoc=0
while read -r hcmd; do
  hcmd="${hcmd#*  }"; hcmd="${hcmd# }"
  [[ -z "$hcmd" ]] && continue
  grep -q "scripts/fleet $hcmd" "$DOC" || { report_fail "help command '$hcmd' documented" "add to AGENTS.md"; undoc=1; }
done < <(printf '%s\n' "$impl_help" | sed -n 's/^  \([a-z][a-z-]*\).*$/\1/p' | sort -u | grep -v registry-check)
[[ $undoc -eq 0 ]] && report_pass "all help commands documented"

for kw in "STATUS SUMMARY" "DOCTOR OK" "DOCTOR FAIL" "APPROVED component=" \
          "PROMOTED component=" "ALREADY AT" "PROMOTE REFUSED" \
          "INIT OK" "INIT ALREADY" "ONBOARDED component=" "NEXT action=" "WO LIST" "VERIFY " \
          "CHECK SUMMARY"; do
  assert_contains "machine contract '$kw' in Go core" "$kw" "$GO_SRC"
done

assert_contains "shim pins FLEET_ROOT" 'export FLEET_ROOT' "$SHIM"
assert_contains "shim builds stale binary via pinned builder" 'ci/build-fleet.sh' "$SHIM"
assert_contains "promote shim delegates to fleet promote" 'promote' "$(cat "$FLEET_ROOT/ci/promote.sh")"

if printf '%s' "$GO_SRC" | grep -q 'KUBECONFIG'; then
  report_fail "Go core never reads KUBECONFIG (rule 7)" "KUBECONFIG token found in cmd/ or internal/"
else
  report_pass "Go core never reads KUBECONFIG (rule 7)"
fi

finalize
