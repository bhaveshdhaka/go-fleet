# HANDOVER — go-fleet program

> Read order for any agent arriving cold: `README.md` → `AGENTS.md` (the
> law) → `PLAN.md` (the program) → this file → `WORKING-NOTES.md` (working
> instructions, ephemeral — deleted at WO-10). Then `git log --oneline -8`.
> Facts below are measured, not inferred.

## State at a glance

| Item | State |
|---|---|
| Repo | `/home/openchamber/workspaces/fleet`, git clean, master; remote `origin` = github.com/bhaveshdhaka/go-fleet (**private**) |
| Corpus | 33 units / 392 assertions, fail=0 skip=0 (`bash scripts/test.sh`); `fleet check` 6/6 PASS |
| Program | `PLAN.md` ACTIVE — next open piece: **WO-9** (site migration) |
| CLI | Go core `cmd/fleet` (module github.com/bhaveshdhaka/go-fleet, `fleet 0.1.0`) behind thin shims `scripts/fleet` + `ci/promote.sh`; binary `dist/fleet` via `ci/build-fleet.sh` |
| Enforcement | front-matter schema v1, predicates P1-P6, full `next` engine, `.fleet.yaml` actor policy (prod human-gated) |
| Distribution | MIT LICENSE; `ci/build-release.sh` static linux/darwin + SHA256SUMS (C11a); install.sh installs `prefix/bin/fleet` (C11b); GH repo private |
| Ops engine | read-only parity (WO-7) **+ MUTATIONS (WO-8)**: `ops build/deploy/rollback/dns[--apply]/monitor/remove/verify` — dual-run with ./lab on live hk-03-dev PASSED with identical results (normalized snapshot diffs S1==S2, S3==S1-modulo-sha, monitor CM data byte-identical); parity oracle frozen as committed goldens (`internal/fleet/testdata/golden/`, labctl 2.0.0); `remove` cleans state entries (journaled deviation #1) and re-renders the tunnel from the updated registry (deviation #2) |
| Drilled VM | RUNNING: QEMU pid `.vm/run/qemu.pid` (23088), `fleet-vm Ready v1.36.3+k3s1`, host API 127.0.0.1:16443 |
| sos-lab | UNTOUCHED at end of WO-8: state files byte-identical to pre-drill baseline, registry restored to committed form; engine remains authoritative until WO-9 cutover |
| Real cluster | hk-03-dev = the host this container runs ON (in-cluster SA `sos-lab/openchamber`). fleet NEVER uses ambient creds — site-declared access only (C12d) |
| WO-8 leftovers | 2 completed `build-canary-*` Jobs + 1 `canary.bhavesh.hk` CNAME (lab-designed residue: ./lab never deletes CNAMEs/jobs; both inert, documented in journal) |

## Rules that got agents burned this cycle (now enforced/asserted)

- Ambient resolution in ANY form: kubectl creds (rule 7) AND repo-root
  walk-up. Fixes: site-declared access + constructed child env (C12d),
  shims pin FLEET_ROOT, drills from neutral cwd with FLEET_ROOT pinned.
- Journal honesty: never append a verify line before reading the summary;
  corrections/findings are `#` lines (append-only, in the open).
- Test scenarios neutralize ALL copied workorders generically.
- kubectl with no HOME litters `.kube/` into the CWD — runner passes temp
  HOME (fixed; keep it fixed).
- **pyyaml rewrite hazard (WO-8)**: any lab `deploy` of a disabled service
  rewrites the whole registry in `yaml.safe_dump` style (block lists,
  folded multi-line plain scalars inside list items). The mini parser must
  read BOTH styles — regression: `TestMiniYamlPyyamlStyle` (C13a).
- **lab quirks mirrored bug-for-bug by design**: deploy PUTs the tunnel
  config before the enabled flip (new hosts enter the tunnel at the next
  `dns --apply`); `./lab status` crashes on null git_sha (no-git builds);
  `./lab remove` leaves deployed.json residue and a stale-routed host.
  fleet matches the first two, fixes the remove-side ones (journaled).

## Next action

Open `PLAN.md`, execute **WO-9** (site migration): `fleet site init --from
sos-lab`; hk-03-dev becomes a fleet-managed site; operational history
archived; secrets untouched. Verify with a post-migration deploy + rollback
drill. Then WO-10 (arjun.hk end-to-end → STOP for owner acceptance).

## Standing cautions

- Secrets: sos-lab `secrets/` are read by fleet only for key-name checks
  and the CF token (transient, never printed). The openchamber gh token
  once flashed into the chat transcript (key-filter slip) — chat-only,
  present in no file; rotate if that matters to you.
- `.vm/` is ~600 MB gitignored; `scripts/vm-tier/down.sh` for graceful
  ACPI shutdown.
- Journal `#` comment lines are the verify/incident channel (doctor and
  C5c/C6c skip them); WO-5's P4 predicate reads `# verify wo=<id>` lines.
- The fleet binary never runs python; the only python in this repo is in
  snapshot DRILL tooling under /tmp (not committed). Goldens freeze the
  python oracle — keep them green.
