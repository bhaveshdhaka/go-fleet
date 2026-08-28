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
    ./scripts/fleet check                  # READ-ONLY predicates P1-P6 report
    ./scripts/fleet site list              # READ-ONLY managed sites registry
    ./scripts/fleet site init <name> --from <dir>
                                           # migrate external site data into
                                           # ops/sites/<name> + cutover SITES.yaml
                                           # (MUTATES repo; predecessor secrets
                                           # COPIED into the secrets home)
    ./scripts/fleet ops <status|doctor>    # READ-ONLY site observation (sos-lab
                                           # parity; explicit access, zero mutations)
    ./scripts/fleet ops register <name> --port N [--host H] [--image I|--repo D]
                                           [--secret KEY]... [--env K=V]...
                                           # register a service in the site
                                           # registry (MUTATES; enabled: false)
    ./scripts/fleet ops build <svc> [--allow-dirty]
                                           # kaniko build (MUTATES cluster+state;
                                           # WO-8 dual-run with ./lab)
    ./scripts/fleet ops deploy <svc>...    # secret+manifests+rollout+dns+tunnel+
                                           # monitor+state (MUTATES)
    ./scripts/fleet ops rollback <svc>     # rollout undo + state record (MUTATES)
    ./scripts/fleet ops remove <svc> [--delete-data] [--unregister]
                                           # teardown; also deletes the service's
                                           # state entries (fleet extension:
                                           # ./lab remove leaves doctor red)
    ./scripts/fleet ops dns [--apply]      # read-only drift report; --apply
                                           # reconciles CNAMEs + tunnel (MUTATES CF)
    ./scripts/fleet ops monitor            # re-render gatus + dashboard (MUTATES CMs)
    ./scripts/fleet ops verify <svc> [--expect N]   # curl the public URL (read-only)
    ./scripts/fleet wo list|show|new ...   # workorder surface (schema v1)
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
   verify/wo new, and since WO-8 the ops mutation verbs build/deploy/
   rollback/remove/dns --apply/monitor) and the numbered block scripts
   under scripts/blocks/ (00–04). No ad-hoc kubectl/curl/mv. Ops
   mutations run DUAL-RUN with ./lab on hk-03-dev until WO-8 parity
   passes (identical results, ./lab doctor ALL CLEAR after every fleet
   mutation); sos-lab stays authoritative until cutover.
2. doctor, status, next, wo list/show, and any --dry-run are read-only.
   init, onboard, wo new, approve, promote and verify mutate.
3. promote RE-RUNS its listed test units right now; stale green logs count
   for nothing. If it refuses, report exact stderr to the owner. Do not
   improvise workarounds.
4. Component onboarding = add one entry in ops/PROJECTS.yaml +
   ci/pipelines/<name>.yaml + gate entries if needed. Doctor must go ALL
   CLEAR before any promotion.
5. Secret VALUES live only in the fleet secrets home —
   `$FLEET_SECRETS_HOME/<site>/` when set, else `$HOME/.fleet/secrets/<site>/`
   — NEVER inside the repo, registry, markdown, journal, or any log
   (house rule, enforced since WO-14: the `secrets_dir` override is
   deleted; `site init --from` COPIES predecessor secrets into the
   secrets home). Key NAMES may appear in the registry and doctor output;
   values may not.
6. Machine contract: every mutating command ends with one summary line
   (STATUS SUMMARY / DOCTOR OK|FAIL / PROMOTED|ALREADY AT / APPROVED) that
   agents parse; keep those formats stable.
7. NEVER rely on ambient cluster credentials. Every kubectl call that is
   not meant for the real lab cluster MUST set KUBECONFIG explicitly
   (e.g. the drill's .vm/run/kubeconfig). An env-level fallback once hit
   hk-03-dev by accident — that failure mode is forbidden.
8. Approval-actor policy lives in .fleet.yaml (WO-5): approvals on
   require_human_stages (default prod) are refused for actors outside
   allowed_actors. Middle hops stay auto-approvable as FLEET_ACTOR=agent.
   Workorder authoring drift is reported by `./scripts/fleet check` /
   `next` (predicates P1-P6) with exact fix commands — it does not block;
   the promote gates remain the only hard blocks.

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
