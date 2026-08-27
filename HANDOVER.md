# HANDOVER — go-fleet program

> Read order for any agent arriving cold: `README.md` → `AGENTS.md` (the
> law) → `PLAN.md` (the program) → this file (live state). Then `git log
> --oneline -12` for recent verified history. Facts below are measured,
> not inferred.

## State at a glance

| Item | State |
|---|---|
| Repo | `/home/openchamber/workspaces/fleet`, git clean, master |
| Corpus | 17 units / 176 assertions, fail=0 skip=0 (`bash scripts/test.sh`) |
| Program | `PLAN.md` ACTIVE — next open piece: **WO-4** (Go core) |
| Module | `github.com/bhaveshdhaka/go-fleet` (repo created at WO-6) |
| Toolchain | pinned via `toolchain.env`; Go 1.27.0 at `~/.toolchain` (PATH) |
| Drilled VM | RUNNING: QEMU pid `.vm/run/qemu.pid`, `fleet-vm Ready v1.36.3+k3s1`, host API `127.0.0.1:16443` (kubeconfig `.vm/run/kubeconfig`) |
| sos-lab | UNTOUCHED, authoritative for hk-03-dev until WO-8 parity; located `../sos-lab` |
| Real cluster | hk-03-dev; mutations via `./lab` ONLY until cutover; explicit KUBECONFIG always (AGENTS.md rule 7) |
| Fleet registry | `ops/PROJECTS.yaml` — fleetctl(prod) + fleethub(built) |

## Rules that got agents burned this cycle (now enforced/asserted)

- Ambient kubeconfig fallback hit the REAL cluster once — never trust env creds (rule 7).
- TCG guests need `virtio-rng-pci` or sshd hangs cloud-init (asserted in C7a).
- Test scratch copies must scrub inherited live state to pristine baseline (C5c/C6c).

## Next action

Open `PLAN.md`, execute **WO-4** (Go core, cmd/fleet) via a workorder:
plan section → decomposed pieces → journaled verify → integrate. The
corpus is the gate — do not integrate on red.

## Standing cautions

- `.vm/` holds ~600 MB vendored QEMU + image; gitignored; safe to delete,
  rebuilt by `scripts/vm-tier/fetch-*.sh` (pinned, sha256-verified).
- `./fleet down` equivalent: `scripts/vm-tier/down.sh` (graceful ACPI).
- Secrets never enter any repo file (sos-lab house rule, inherited).
