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
    ./scripts/fleet next                   # READ-ONLY guidance: next legal action
    ./scripts/fleet wo list|show|new ...   # workorder surface (minimal)
    ./scripts/fleet init [dir]             # scaffold the SDLC file skeleton
    ./scripts/fleet onboard <name>         # register component (+pipeline+state)
    ./scripts/fleet verify [units...]      # run corpus units, journal the result
    ./scripts/fleet approve <c> <dev|prod> # write sign-off file + journal line
    ./scripts/fleet promote <c> <stage>    # gated transition; refuses when
                                           # [--dry-run] [--skip-gates] # unsafe
    bash scripts/test.sh [units...]        # deterministic DAG test runner

Since WO-4 the CLI is the Go core (cmd/fleet) behind thin shims
(scripts/fleet, ci/promote.sh); the bash corpus stays the black-box test
spine.

## Rules

1. Mutations happen ONLY through ./fleet (init/onboard/approve/promote/
   verify/wo new) and the numbered block scripts under scripts/blocks/
   (00–04). No ad-hoc kubectl/curl/mv.
2. doctor, status, next, wo list/show, and any --dry-run are read-only.
   init, onboard, wo new, approve, promote and verify mutate.
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
7. NEVER rely on ambient cluster credentials. Every kubectl call that is
   not meant for the real lab cluster MUST set KUBECONFIG explicitly
   (e.g. the drill's .vm/run/kubeconfig). An env-level fallback once hit
   hk-03-dev by accident — that failure mode is forbidden.

## Execution tiers

| Tier | What | Where | Entry |
|---|---|---|---|
| 0 | hermetic corpus (no net, no cluster) | this container | `bash scripts/test.sh` |
| 1 | real Ubuntu VM + real k3s + live apply/rollback | userspace QEMU on this host | `scripts/test-onvm.sh --with-vm` |
| 2 | the actual lab host drill | disposable Ubuntu server | future |

Tier-1 scripts: `scripts/vm-tier/{fetch-qemu,fetch-image,up,down,build-image-tar}.sh`
(vendor prefix under gitignored `.vm/`; no sudo anywhere).

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
