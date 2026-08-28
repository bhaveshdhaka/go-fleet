# HANDOVER — go-fleet program

> Read order for any agent arriving cold: `README.md` → `AGENTS.md` (the
> law) → `PLAN.md` (the program) → this file → `WORKING-NOTES.md` (working
> instructions, ephemeral — deleted at WO-10). Then `git log --oneline -8`.
> Facts below are measured, not inferred.

## State at a glance

| Item | State |
|---|---|
| Repo | `/home/openchamber/workspaces/fleet`, git clean, master; remote `origin` = github.com/bhaveshdhaka/go-fleet (**private**) |
| Corpus | 35 units / 433 assertions, fail=0 skip=0 (`bash scripts/test.sh`); `fleet check` 6/6 PASS |
| Program | `PLAN.md` ACTIVE — next open piece: **WO-10** (arjun.hk end-to-end → STOP for owner acceptance) |
| CLI | Go core `cmd/fleet` (module github.com/bhaveshdhaka/go-fleet, `fleet 0.1.0`) behind thin shims `scripts/fleet` + `ci/promote.sh`; binary `dist/fleet` via `ci/build-fleet.sh` |
| Enforcement | front-matter schema v1, predicates P1-P6, full `next` engine, `.fleet.yaml` actor policy (prod human-gated) |
| Distribution | MIT LICENSE; `ci/build-release.sh` static linux/darwin + SHA256SUMS (C11a); install.sh installs `prefix/bin/fleet` (C11b); GH repo private |
| Ops engine | read-only parity (WO-7) + mutations dual-run PASSED (WO-8) + **SITE MIGRATED (WO-9)**: hk-03-dev is `engine: fleet`, data at `ops/sites/hk-03-dev/` (registry/state/templates tracked in git), secrets REFERENCED at `../sos-lab/secrets` (untouched, never copied), history archived with MIGRATION manifest; `site init --from` + `ops register` landed; deploy self-reconciles DNS/tunnel post-flip (deviation #3); post-migration deploy v1→v2→rollback drill verified live via fleet alone |
| Drilled VM | RUNNING: QEMU pid `.vm/run/qemu.pid` (23088), `fleet-vm Ready v1.36.3+k3s1`, host API 127.0.0.1:16443 |
| sos-lab | FROZEN ARCHIVE since WO-9: tree byte-identical to post-WO-8 baseline; its `./lab` reports only on its own (stale) data — fleet ops is the arbiter now |
| Real cluster | hk-03-dev = the host this container runs ON (in-cluster SA `sos-lab/openchamber`). fleet NEVER uses ambient creds — site-declared access only (C12d) |
| Drill leftovers | 4 completed `build-canary-*` Jobs (2×WO-8, 2×WO-9) + 1 `canary.bhavesh.hk` CNAME — inert, lab-designed residue, documented in journal |

## Rules that got agents burned this cycle (now enforced/asserted)

- Ambient resolution in ANY form: kubectl creds (rule 7) AND repo-root
  walk-up. Fixes: site-declared access + constructed child env (C12d),
  shims pin FLEET_ROOT, drills from neutral cwd with FLEET_ROOT pinned.
- Journal honesty: never append a verify line before reading the summary;
  corrections/findings are `#` lines (append-only, in the open).
- Test scenarios neutralize ALL copied workorders generically.
- kubectl with no HOME litters `.kube/` into the CWD — runner passes temp
  HOME (fixed; keep it fixed).
- **pyyaml rewrite hazard (WO-8)**: any deploy of a disabled service
  rewrites the whole registry in `yaml.safe_dump` style. The mini parser
  reads BOTH styles — regression: `TestMiniYamlPyyamlStyle` (C13a).
- **Secrets-dir plumbing (WO-9)**: `LoadCloudflareToken`/`labDashboardSlug`
  take the SECRETS DIR (they append the filename themselves); every call
  site must go through `site.secretsDir(root)` — doubled-path bug caught
  live by the doctor precondition. Regression: `TestSecretsDirOverride`.
- **labServiceBlock absent-name bug (WO-9)**: named-return start defaults
  to 0 — an absent name "matched" block [0,1). Always refuse with
  "not registered" (test asserts the exact message).
- **lab quirks mirrored bug-for-bug by design**: deploy PUTs the tunnel
  config before the enabled flip (new hosts enter the tunnel at the next
  `dns --apply`); `./lab status` crashes on null git_sha (no-git builds);
  `./lab remove` leaves deployed.json residue and a stale-routed host.
  fleet matches the first two, fixes the remove-side ones (journaled).

## Next action

Open `PLAN.md`, execute **WO-10**: `fleet init arjun-hk`; single-page Go
site (dollarbucks menu, iOS/Safari/retina contract tests); gated promote;
`ops deploy` to hk-03-dev (fleet-managed); DNS arjun.hk; monitoring.
Ends with an evidenced master checklist and a **STOP for owner
acceptance**. arjun.hk zone_id already exists in the site registry
domains.

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
