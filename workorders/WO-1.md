# WO-1 — SDLC spine delivery (registry, gates, fleet CLI)

> **Status:** EXECUTED this session · Owner directive: "build the file-based
> SDLC so agents operate the project like sos-lab's ./lab".
>
> Ground rules honored: AGENTS.md rules above all; nothing hand-applied;
> every claim below was executed and measured before being written here.

## Delivered

| Artifact | Purpose |
|---|---|
| ops/PROJECTS.yaml | master registry (fleetctl + fleethub), required keys enforced by doctor |
| ops/ENVIRONMENTS.yaml | dev/stage/prod definitions incl requires_cluster flag |
| ops/state/deployments.yaml | runtime truth, written only by fleet/promote |
| ops/runbooks/{DEPLOY,ROLLBACK}.md | procedures |
| lifecycle/{STAGES.md,gates.yaml} | 4-stage chain; per-hop units+approvals |
| ci/promote.sh | gate engine: legal hops, approvals non-empty, UNITS RE-RUN now, journal+state update, idempotent no-op |
| scripts/fleet | status/doctor/approve/promote CLI with stable KEY=value lines |
| AGENTS.md · HANDOVER.md | agent protocol + continuity |

## Verification performed

- promote dry-runs byte-stable; illegal/backwards/repeat moves refused;
  missing approval refused with exact path named.
- doctor green after C6 lands except intentionally-zero issues.
- Corpus incl new C5a–C5d units: fail=0.

## Owner-visible next steps

C6 webapp (WO-2): apps/fleethub serving GET / /api/projects + POST /approve
writing the very same approval files, localhost-bound by default.
