# fleet control plane (agent protocol v1)

Two-file rule of this repo:
  - Deployment INTENT lives in `ops/PROJECTS.yaml` (+ ENVIRONMENTS/gates).
  - Runtime TRUTH lives in `ops/state/deployments.yaml`.
    State is written ONLY by `scripts/fleet`. Never hand-edit state,
    never sed/kubectl your way around a refused transition.

## The only commands (repo root):

    ./scripts/fleet status [component]     # registered components + stage
    ./scripts/fleet doctor                 # READ-ONLY drift check; run FIRST
                                           # whenever anything looks wrong
    ./scripts/fleet approve <c> <dev|prod> # write sign-off file + journal line
    ./scripts/fleet promote <c> <stage>    # gated transition; refuses when
                                           # [--dry-run] [--skip-gates] # unsafe
    bash scripts/test.sh [units...]        # deterministic DAG test runner

## Rules

1. Mutations happen ONLY through ./fleet (approve/promote) and the numbered
   block scripts under scripts/blocks/ (00–04). No ad-hoc kubectl/curl/mv.
2. doctor, status, and any --dry-run are read-only. Everything else mutates.
3. promote RE-RUNS its listed test units right now; stale green logs count
   for nothing. If it refuses, report exact stderr to the owner. Do not
   improvise workarounds.
4. Component onboarding = add one entry in ops/PROJECTS.yaml +
   ci/pipelines/<name>.yaml + gate entries if needed. Doctor must go ALL
   CLEAR before any promotion.
5. Secret values live only in gitignored files outside git — NEVER inside
   registry/markdown/journal (house rule inherited from sos-lab).
6. Machine contract: every mutating command ends with one summary line
   (STATUS SUMMARY / DOCTOR OK|FAIL / PROMOTED|ALREADY AT / APPROVED) that
   agents parse; keep those formats stable.

## Process artifacts

    lifecycle/STAGES.md      stage meanings + no-shortcut rules
    lifecycle/gates.yaml     which units + approvals each hop needs
    ops/runbooks/*.md        deploy / rollback procedures
    workorders/WO-*.md       self-contained execution briefs (status header)
    HANDOVER.md              measured end-of-session state, always updated

## House build standard

Go-first static binaries (zero JS runtimes), pinned toolchain from
toolchain.env via scripts/blocks/02-toolchain.sh into ./.toolchain.
Full hermetic tier requires neither network nor a cluster.
