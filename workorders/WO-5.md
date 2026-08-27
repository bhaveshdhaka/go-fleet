---
wo: WO-5
title: Enforcement v1
status: EXECUTED
plan: PLAN.md
pieces:
  - id: 1
    title: front-matter schema + wo surface + retrofit WO-1..4
    verify: go vet + go test ./internal/fleet + bash scripts/test.sh
    integrated: true
  - id: 2
    title: predicates P1-P6 + fleet check + FLEET_WO verify tagging
    verify: go vet + go test ./internal/fleet + bash scripts/test.sh
    integrated: true
  - id: 3
    title: next guidance engine + approval-actor policy (.fleet.yaml)
    verify: go vet + go test ./internal/fleet + bash scripts/test.sh
    integrated: true
  - id: 4
    title: C10a-C10d units + corpus contract refresh + full corpus gate
    verify: bash scripts/test.sh (C10 units green + full corpus)
    integrated: true
---

# WO-5 — Enforcement v1: predicates, schema, guidance, actor policy

> **Status:** EXECUTED this session · Owner directive: execute PLAN.md
> WO-5 through the house process (goal message 2026-08-27 grants scope/plan
> approval for WO-5..WO-10; the only mandatory stop is arjun.hk prod
> acceptance at the end of WO-10).

## Measured results (all verified this session, journaled as # verify lines)

| Piece | Verify | Result |
|---|---|---|
| 1 schema + wo surface + retrofit | vet+test+corpus | 5 WOs schema=v1; 21u/257a fail=0 |
| 2 predicates P1-P6 + fleet check | check 6/6 + corpus | one red cycle: C9d caught undocumented `check`; AGENTS.md updated in-cycle; 21u/257a fail=0 |
| 3 next engine + actor policy | policy refusal/allow + corpus | agent refused on prod w/ exact fix; dev auto-OK; 21u/257a fail=0 |
| 4 C10a-C10d + full corpus | targeted then full | C10 pass=35; **25u/292a fail=0 skip=0** |

Three test-scenario bugs were fixed in-cycle during C10 bring-up (each
proved the engine correct: partial integration restore, P1 priority over
lifecycle, built-stage needing no approval).

## Plan section (decisions, dated 2026-08-27)

- Front-matter schema v1: `wo/title/status/plan` + `pieces` list (`id`,
  `title`, `verify`, `integrated`). Markdown stays data the binary
  validates. WO-1..WO-4 are retrofitted with front-matter (data migration,
  statuses unchanged) so no grandfathering hacks are needed; legacy parsing
  (the `> **Status:**` prose header) stays as fallback for files without
  front-matter.
- Predicates are FILE-STATE checks surfaced by `fleet check` and the `next`
  engine as `CHECK P<N> PASS|FAIL|SKIP detail=...` with exact fix commands.
  Per the enforcement-policy decision (PLAN), authoring drift is REPORTED,
  not blocking; the promote gates remain the only hard blocks.
  - P1 dirty-tree-without-WO: `git status --porcelain` non-empty requires
    ≥1 workorder with status OPEN|IN_PROGRESS. Not a git repo → SKIP.
  - P2 missing plan link: every front-matter workorder needs a non-empty
    `plan:` referencing PLAN.md.
  - P3 missing decomposition: every front-matter workorder needs ≥1 piece.
  - P4 unjournaled verify: every IN_PROGRESS workorder needs ≥1
    `# verify wo=<id>` line in the journal (EXECUTED history stays as-is;
    no retro-evidence demanded — that would fabricate audits).
  - P5 unintegrated pieces: every EXECUTED front-matter workorder needs all
    pieces `integrated: true`.
  - P6 journal tamper: `git diff HEAD -- lifecycle/journal/events.log` must
    only ADD lines; any removed/modified `ts=` line is a rewrite → FAIL.
    Not a git repo → SKIP.
- `next` guidance engine (replaces WO-4 minimal): evaluates P1..P6 in
  order, guides on the first failing predicate with `NEXT predicate=P<N>`,
  then falls through to the lifecycle suggestion (W4 logic).
- Approval-actor policy in `.fleet.yaml`: `approvals.allowed_actors` +
  `approvals.require_human_stages`. `fleet approve` refuses human-stage
  approvals by non-allowed actors with the exact fix (FLEET_ACTOR=owner).
  Missing .fleet.yaml = unrestricted (compat). Policy enforced at the CLI
  writer; the localhost webapp is the owner's own tool (documented
  boundary). Middle hops stay auto-approvable as FLEET_ACTOR=agent.
- `fleet verify` honors FLEET_WO to tag its journal line (`wo=<id>`) so P4
  has an in-product evidence path.
- init scaffold gains `.fleet.yaml` (files=6 → 7; C9c assert refreshed).

## Pieces (each: verify → journal → integrate; corpus green required)

1. Front-matter schema + parser + wo list/new/show + retrofit WO-1..4 +
   workorder/init templates. verify: go vet + go test + corpus.
2. Predicates P1-P6 + `fleet check` + FLEET_WO tagging in verify.
   verify: go vet + go test + corpus.
3. `next` engine + `.fleet.yaml` policy in approve + help/AGENTS.md.
   verify: go vet + go test + corpus.
4. C10a frontmatter / C10b predicates / C10c next engine / C10d actor
   policy + C9c files=7 + C5d `check` + full corpus gate.

## Standing guardrails

- QEMU drill VM left running; sos-lab authoritative for hk-03-dev until
  WO-8 parity; zero cluster operations in WO-5; no secrets in repo files.
