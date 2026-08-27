#!/usr/bin/env bash
# ci/promote.sh — gate-enforcing stage transition.
#
# CONTRACT:
#   promote <component> <to-stage> [--dry-run] [--skip-gates]
#   rc=0 performed | no-op | dry-run ;   rc=1 refused.
#   Refusals  -> `PROMOTE REFUSED :: reason` lines on stderr.
#   Performed -> `PROMOTED component=<c> from=<x> to=<y> at=<ts>` on stdout.
#   No-op     -> `ALREADY AT component=<c> stage=<s>`.
# Enforces adjacent/dev->prod moves only, approval files non-empty, required
# gate test units RE-RUN green right now (unless --skip-gates), one journal
# append, exact-block state rewrite. Repeat promotes mutate nothing.

set -uo pipefail
FLEET_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export FLEET_ROOT
# shellcheck disable=SC1091
source "$FLEET_ROOT/toolchain.env"

die() { printf 'PROMOTE REFUSED :: %s\n' "$1" >&2; exit 1; }

usage="usage: promote <component> <to-stage> [--dry-run] [--skip-gates]"
[[ $# -ge 2 ]] || { printf '%s\n' "$usage" >&2; exit 2; }
component=$1; to_stage=$2; shift 2
dry_run=false skip_gates=false
for a in "$@"; do
  case "$a" in
    --dry-run)    dry_run=true ;;
    --skip-gates) skip_gates=true ;;
    *) printf 'unknown flag %s\n%s\n' "$a" "$usage" >&2; exit 2 ;;
  esac
done

REG="$FLEET_ROOT/ops/PROJECTS.yaml"
STATE="$FLEET_ROOT/ops/state/deployments.yaml"
GATES="$FLEET_ROOT/lifecycle/gates.yaml"
JOURNAL="$FLEET_ROOT/lifecycle/journal/events.log"
APPR="$FLEET_ROOT/lifecycle/approvals"

grep -Eq "^  - name: ${component}[[:space:]]*\$" "$REG" || die "unknown component '$component'"
case "$to_stage" in built|dev|stage|prod) : ;; *) die "illegal target stage '$to_stage'" ;; esac

current_stage() {
  awk -v c="$component" '
    index($0,"  - name: "c)==1 {inblk=1; next}
    inblk && /^  - name:/ {exit}
    inblk && match($0,/stage:[[:space:]]*/) {print substr($0,RSTART+RLENGTH); exit}
  ' "$STATE"
}
from_stage="$(current_stage)"
[[ -n "$from_stage" ]] || die "no current stage found for '$component'"

if [[ "$from_stage" == "$to_stage" ]]; then
  [[ $dry_run == true ]] \
    && { printf '[promote][dry-run] already at %s (no mutation)\n' "$to_stage"; exit 0; }
  printf 'ALREADY AT component=%s stage=%s\n' "$component" "$to_stage"
  exit 0
fi

case "|${from_stage}|${to_stage}|" in
  "|built|dev|"|"|dev|stage|"|"|stage|prod|"|"|dev|prod|") : ;;
  *) die "illegal transition '$from_stage' -> '$to_stage'" ;;
esac

# ---- parse gates.yaml: emit "U <unit>" and "A <approval>" for this edge ----
gate_items() {
  awk -v FS=':' -v fs="$from_stage" -v ts="$to_stage" '
    /^[[:space:]]*-[[:space:]]+from:/ {
      gsub(/[[:space:]]/, "", $0); v=$0; sub(/^.*from:/, "", v)
      f=v; want=0; have_to=0; next
    }
    /^    to:/ {
      gsub(/[[:space:]]/, "", $0); v=$0; sub(/^.*to:/, "", v)
      if ($0 ~ /^[[:space:]]*to:/) {}
      tw=v; want=(f==fs && tw==ts); have_to=1; sec=0; next
    }
    /^\#[^!]*$/ { next }                      # comment-only lines
    /^[[:alpha:]_]/ { sec=0; next }           # new top-level key ends gate
    want {
      if (/requires_units:/)  { sec="U"; next }
      if (/needs_approvals:/) { sec="A"; next }
      if (sec != "" && /^[[:space:]]+-[[:space:]]/) {
        item=$0; sub(/^[[:space:]]*-[[:space:]]*/, "", item);
        gsub(/[[:space:]]/, "", item)
        if (item != "") print sec" "item
      }
    }
  ' "$GATES"
}
units_needed()     { gate_items | sed -n 's/^U //p'; }
approvals_needed() { gate_items | sed -n 's/^A //p'; }

rc_missing=0
while IFS= read -r s; do
  [[ -z "$s" ]] && continue
  if [[ ! -s "$APPR/$s/$component.approved" ]]; then
    printf 'PROMOTE REFUSED :: missing approval file %s\n' "$APPR/$s/$component.approved" >&2
    rc_missing=1
  fi
done < <(approvals_needed)
[[ $rc_missing -eq 0 ]] || exit 1

unit_list=()
while IFS= read -r u; do
  [[ -z "$u" ]] && continue
  [[ -d "$FLEET_ROOT/tests/$u" ]] || die "gate references unknown unit '$u'"
  unit_list+=("$u")
done < <(units_needed)

if [[ $dry_run == true ]]; then
  printf '[promote][dry-run] would enforce approvals: %s\n' "$(approvals_needed | tr '\n' ' ')"
  printf '[promote][dry-run] would re-run units: %s\n' "${unit_list[*]:-(none)}"
  printf '[promote][dry-run] would move %s: %s -> %s\n' "$component" "$from_stage" "$to_stage"
  exit 0
fi

if [[ $skip_gates == false && ${#unit_list[@]} -gt 0 ]]; then
  out="$(bash "$FLEET_ROOT/scripts/test.sh" "${unit_list[@]}" 2>&1)" || true
  fails="$(printf '%s\n' "$out" | sed -n 's/^FLEET SUMMARY.*fail=\([0-9]*\).*$/\1/p' | tail -1)"
  if [[ "${fails:-}" != "0" ]]; then
    head_fails="$(printf '%s\n' "$out" | grep -E '^\[FAIL\]' | head -3 | tr '\n' ';')"
    die "gate suite failed (fail=${fails:-unknown}). ${head_fails}"
  fi
fi

ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

tmp_state="$(mktemp)"
awk -v c="$component" -v ns="$to_stage" -v nts="$ts" '
  index($0,"  - name: "c)==1            { inblk=1; print; next }
  inblk && /^  - name:/                  { inblk=0 }
  inblk && /^[[:space:]]*stage:/         { sub(/:.*/, ": " ns) }
  inblk && /^[[:space:]]*last_promoted_at:/ { sub(/:.*/, ": \"" nts "\"") }
  { print }
' "$STATE" >"$tmp_state" || die "state render failed"

printf 'ts=%s event=promoted component=%s from=%s to=%s actor=%s\n' \
  "$ts" "$component" "$from_stage" "$to_stage" "${FLEET_ACTOR:-agent}" >>"$JOURNAL"

mv "$tmp_state" "$STATE"

printf 'PROMOTED component=%s from=%s to=%s at=%s\n' "$component" "$from_stage" "$to_stage" "$ts"
