#!/usr/bin/env bash
# C5d — agent protocol integrity (AGENTS.md ↔ ./fleet reality).
# The documented command surface in AGENTS.md must match exactly what
# scripts/fleet implements, and the machine-parse summary keywords the doc
# promises must exist verbatim in the implementing code. Static.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"

DOC="$FLEET_ROOT/AGENTS.md"
CLI="$FLEET_ROOT/scripts/fleet"
PROMOTE="$FLEET_ROOT/ci/promote.sh"

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

# inverse: help must not offer undocumented mutating commands beyond promote
for hcmd in status doctor approve promote registry-check; do
  grep -qE "^[[:space:]]+\.?/?[[:space:]]*(\./)?(scripts/fleet )?$hcmd" "$DOC" \
    || case "$hcmd" in
         registry-check) : ;; # alias, allowed to be doc-light
         *) report_fail "help exposes undocumented command '$hcmd'" "add to AGENTS.md" ;;
       esac
done

# machine contract keywords live where they are promised
for kw in "STATUS SUMMARY" "DOCTOR OK" "APPROVED"; do
  assert_contains "CLI emits $kw" "$kw" "$(cat "$CLI")"
done
for kw in "PROMOTED component=" "ALREADY AT" "PROMOTE REFUSED"; do
  assert_contains "promote.sh emits $kw" "$kw" "$(cat "$PROMOTE")"
done

# rules section guards the mutation chokepoint & secrets house rule
assert_contains "AGENTS.md locks mutations to ./fleet" \
  "ONLY through ./fleet" "$(cat "$DOC")"
assert_contains "AGENTS.md inherits secrets rule" \
  "NEVER inside" "$(cat "$DOC")"

finalize
