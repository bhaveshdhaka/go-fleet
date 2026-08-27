# HANDOVER — go-fleet program

> Read order for any agent arriving cold: `README.md` → `AGENTS.md` (the
> law) → `PLAN.md` (the program) → this file → `WORKING-NOTES.md` (working
> instructions, ephemeral — deleted at WO-10). Then `git log --oneline -8`.
> Facts below are measured, not inferred.

## State at a glance

| Item | State |
|---|---|
| Repo | `/home/openchamber/workspaces/fleet`, git clean, master; remote `origin` = github.com/bhaveshdhaka/go-fleet (**private**) |
| Corpus | 31 units / 347 assertions, fail=0 skip=0 (`bash scripts/test.sh`) |
| Program | `PLAN.md` ACTIVE — next open piece: **WO-8** (ops mutations, dual-run with ./lab) |
| CLI | Go core `cmd/fleet` (module github.com/bhaveshdhaka/go-fleet, `fleet 0.1.0`) behind thin shims `scripts/fleet` + `ci/promote.sh`; binary `dist/fleet` via `ci/build-fleet.sh` |
| Enforcement | front-matter schema v1 (incl. `integrated: deferred` for owner-waived pieces), predicates P1-P6 (`fleet check` — 6/6 PASS), full `next` engine, `.fleet.yaml` actor policy (prod human-gated) |
| Distribution | MIT LICENSE; `ci/build-release.sh` static linux/darwin + SHA256SUMS (C11a); install.sh installs `prefix/bin/fleet` (C11b); GH repo private |
| Ops engine | `ops/SITES.yaml` (hk-03-dev, engine sos-lab, access in-cluster — EXPLICIT, never ambient); `fleet ops status/doctor` **byte-identical with ./lab on live hk-03-dev** (status+doctor text, doctor --json); ZERO mutations performed |
| Toolchain | go 1.27.0 pinned (toolchain.env); gh 2.98.0 at `~/.local/bin/gh` (NOT on default PATH), auth via openchamber token (`GH_TOKEN` pattern — see WORKING-NOTES) |
| Drilled VM | RUNNING: QEMU pid `.vm/run/qemu.pid` (23088), `fleet-vm Ready v1.36.3+k3s1`, host API 127.0.0.1:16443 |
| sos-lab | UNTOUCHED except its read-only verbs; authoritative for hk-03-dev until WO-8 dual-run parity passes |
| Real cluster | hk-03-dev = the host this container runs ON (in-cluster SA `sos-lab/openchamber`). fleet NEVER uses ambient creds — site-declared access only (C12d) |

## Rules that got agents burned this cycle (now enforced/asserted)

- Ambient resolution in ANY form: kubectl creds (rule 7) AND repo-root
  walk-up (WO-4 live-registry incident). Fixes: site-declared access +
  constructed child env (C12d), shims pin FLEET_ROOT, drills from neutral
  cwd with FLEET_ROOT pinned.
- Journal honesty: never append a verify line before reading the summary;
  corrections are `# journal-correction` lines (append-only, in the open).
- Test scenarios neutralize ALL copied workorders generically, else every
  new WO file breaks the corpus.
- kubectl with no HOME litters `.kube/` into the CWD — runner passes temp
  HOME (fixed; keep it fixed).

## Next action

Open `PLAN.md`, execute **WO-8** (ops mutations): deploy/build/dns/monitor
via fleet, DUAL-RUN with ./lab on hk-03-dev until identical, cutover only
when both paths agree. Then WO-9 (migration), WO-10 (arjun.hk end-to-end →
STOP for owner acceptance). Workorder process per house law; corpus is the
gate. WORKING-NOTES.md has the WO-8 quick-start.

## Standing cautions

- Secrets: sos-lab `secrets/` are read by fleet only for key-name checks
  and the CF token (transient, never printed). The openchamber gh token
  once flashed into the chat transcript (key-filter slip) — chat-only,
  present in no file; rotate if that matters to you.
- `.vm/` is ~600 MB gitignored; `scripts/vm-tier/down.sh` for graceful
  ACPI shutdown.
- Journal `#` comment lines are the verify/incident channel (doctor and
  C5c/C6c skip them); WO-5's P4 predicate reads `# verify wo=<id>` lines.
