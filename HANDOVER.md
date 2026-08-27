# HANDOVER — go-fleet program

> Read order for any agent arriving cold: `README.md` → `AGENTS.md` (the
> law) → `PLAN.md` (the program) → this file (live state). Then `git log
> --oneline -12` for recent verified history. Facts below are measured,
> not inferred.

## State at a glance

| Item | State |
|---|---|
| Repo | `/home/openchamber/workspaces/fleet`, git clean, master |
| Corpus | 21 units / 257 assertions, fail=0 skip=0 (`bash scripts/test.sh`) |
| Program | `PLAN.md` ACTIVE — next open piece: **WO-5** (enforcement v1) |
| CLI | **Go core live**: `cmd/fleet` (module `github.com/bhaveshdhaka/go-fleet`) behind thin shims `scripts/fleet` + `ci/promote.sh`; binary `dist/fleet` (gitignored), built by `ci/build-fleet.sh` (pinned go1.27.0, GOPROXY=off, trimpath, byte-reproducible) |
| New WO-4 commands | `init onboard next wo verify` (minimal — WO-5 adds front-matter schema, predicates P1-P6, full guidance engine) |
| Toolchain | pinned via `toolchain.env`; Go 1.27.0 at `~/.toolchain` (PATH) |
| Drilled VM | RUNNING: QEMU pid `.vm/run/qemu.pid` (23088 at handover), `fleet-vm Ready v1.36.3+k3s1`, host API `127.0.0.1:16443` (kubeconfig `.vm/run/kubeconfig`) |
| sos-lab | UNTOUCHED, authoritative for hk-03-dev until WO-8 parity; located `../sos-lab` |
| Real cluster | hk-03-dev; mutations via `./lab` ONLY until cutover; explicit KUBECONFIG always (AGENTS.md rule 7); the Go core never reads KUBECONFIG (asserted by C9d) |
| Fleet registry | `ops/PROJECTS.yaml` — fleetctl(prod) + fleethub(built) |

## Rules that got agents burned this cycle (now enforced/asserted)

- Ambient kubeconfig fallback hit the REAL cluster once — never trust env
  creds (rule 7). NEW (WO-4): same hazard exists for repo resolution — a
  drill with empty FLEET_ROOT from the repo cwd let `onboard` append to the
  LIVE registry (repaired, journaled). Shims now pin FLEET_ROOT; drills
  must run from a neutral cwd with FLEET_ROOT pointing at the scratch copy.
- TCG guests need `virtio-rng-pci` or sshd hangs cloud-init (asserted in C7a).
- Test scratch copies must scrub inherited live state to pristine baseline (C5c/C6c).
- Onboard is not atomic: pipeline file renders FIRST, then registry/state
  appends — template errors must surface before any contract mutation.

## Next action

Open `PLAN.md`, execute **WO-5** (enforcement v1): workorder front-matter
schema; predicates P1-P6; `fleet next` guidance engine replaces the WO-4
minimal one; approval-actor policy in .fleet.yaml. Workorder process:
plan section → decomposed pieces → journaled verify (`fleet verify` already
journals `# verify` lines) → integrate. The corpus is the gate — do not
integrate on red.

## Standing cautions

- `.vm/` holds ~600 MB vendored QEMU + image; gitignored; safe to delete,
  rebuilt by `scripts/vm-tier/fetch-*.sh` (pinned, sha256-verified).
- `./fleet down` equivalent: `scripts/vm-tier/down.sh` (graceful ACPI).
- Secrets never enter any repo file (sos-lab house rule, inherited).
- Journal `#` comment lines are the verify/incident record channel (WO-5
  P4 will formalize verify events; doctor/C5c/C6c skip comments).
