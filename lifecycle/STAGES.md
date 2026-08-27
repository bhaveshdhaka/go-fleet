# lifecycle/STAGES.md — component lifecycle (human + agent readable)

Every registered component walks the same one-way chain:

    built ──(verify)──► dev ──(approve:dev)──► stage ──(approve:prod)──► prod

## Stage meanings
| Stage | Meaning | Where |
|---|---|---|
| built | hermetic build green, nothing deployed yet | Tier-0 container |
| dev   | manifests rehearsed client-side; functional smoke OK | Tier-0 container |
| stage | post-dev approval recorded; full offline gate suite re-run green | Tier-0 container |
| prod  | approved for the real k3s host; applies there via VM tier | VM drill |

## Rules
1. Transitions happen ONLY through `./fleet promote <component> <to-stage>`.
2. `promote` re-runs every test unit listed in that transition's gate at the
   moment of promotion — stale greens do not count.
3. Approvals are plain files under lifecycle/approvals/<stage>/<component>.approved.
4. Every mutation appends exactly one line to lifecycle/journal/events.log
   (append-only audit trail). Repeat promote of an already-current stage is a
   no-op that mutates nothing.
5. Backwards motion (rollbacks) use event=rejected journal entries and are
   escalated to the owner in HANDOVER.md — single-operator lab convention.
