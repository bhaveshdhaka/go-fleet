# HANDOVER — go-fleet program

> Read order for any agent arriving cold: `README.md` → `AGENTS.md` (the
> law) → `PLAN.md` (the program) → this file. Then `git log --oneline -8`.
> (WORKING-NOTES.md was the ephemeral WO-5..WO-10 working file; deleted at
> WO-10 with everything worthy codified here and in AGENTS.md.)
> Facts below are measured, not inferred.

## State at a glance

| Item | State |
|---|---|
| Repo | `/home/openchamber/workspaces/fleet`, git clean, master; remote `origin` = github.com/bhaveshdhaka/go-fleet (**private**) |
| Corpus | 44 units / 550 assertions, fail=0 skip=0 (`bash scripts/test.sh`); `fleet check` 6/6 PASS |
| Program | `PLAN.md` completion program WO-14..WO-19 **EXECUTED** (owner directive "no more sos lab, only go fleet") — secrets divorce, fresh-install path, rich register + next-engine ship path, --json surface, onboarding docs, secrets audit + release |
| CLI | Go core `cmd/fleet` (module github.com/bhaveshdhaka/go-fleet, `fleet 0.1.0`) behind thin shims `scripts/fleet` + `ci/promote.sh`; binary `dist/fleet` via `ci/build-fleet.sh` |
| Enforcement | front-matter schema v1, predicates P1-P6, full `next` engine, `.fleet.yaml` actor policy (prod human-gated) |
| Distribution | MIT LICENSE; `ci/build-release.sh` static linux/darwin + SHA256SUMS (C11a); install.sh installs `prefix/bin/fleet` (C11b); GH repo private |
| Ops engine | read-only parity (WO-7) + mutations dual-run PASSED (WO-8) + site migrated (WO-9): hk-03-dev is `engine: fleet`, data at `ops/sites/hk-03-dev/` (git-tracked); WO-14: secrets home `$HOME/.fleet/secrets/hk-03-dev/` (7 env files, 0700/0600, source sos-lab untouched), `secrets_dir` override DELETED from the schema, `site init --from` copies predecessor secrets; deploy secret-creation + envFrom now resolve through the site secrets dir (latent live bug fixed: services with declared secrets would have silently skipped secret creation) |
| arjun-hk | **LIVE at https://arjun.hk** (WO-10): onboarded component (apps/arjun-hk, port 8080), gated promote built→dev→stage→prod (prod approval owner-via-agent per .fleet.yaml, owner directive journaled), kaniko multi-stage image, deployed to hk-03-dev, CNAME arjun.hk retargeted to the lab tunnel, gatus+dashboard monitoring (8 endpoints), HTTP 200 + served-content verified; contract tests C15a (iOS/Safari/retina/menu) run the served binary over loopback |
| Drilled VM | RUNNING: QEMU pid `.vm/run/qemu.pid` (23088), `fleet-vm Ready v1.36.3+k3s1`, host API 127.0.0.1:16443 |
| sos-lab | FROZEN ARCHIVE since WO-9: tree byte-identical to post-WO-8 baseline; its `./lab` reports only on its own (stale) data — fleet ops is the arbiter now |
| Real cluster | hk-03-dev = the host this container runs ON (in-cluster SA `sos-lab/openchamber`). fleet NEVER uses ambient creds — site-declared access only (C12d) |
| Drill leftovers | 4 completed `build-canary-*` Jobs + 1 `canary.bhavesh.hk` CNAME + 1 completed `build-arjun-hk-*` failed-job pod (first kaniko attempt, pre-fix) — inert, documented in journal |
| STOP | WO-10 ends HERE: master checklist in workorders/WO-10.md all evidenced; **awaiting owner acceptance** — no further mutations |

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

**Owner acceptance of the WO-14..WO-19 completion program.** Delivery
highlights: secrets home (one mechanism, outside the repo, hk-03-dev
migrated), BOOTSTRAP token STORAGE fix, fresh-install path
(`site new`/`site tunnel create`/`infra deploy`/`site canary` — LIVE
CANARY PASS on hk-03-dev with public HTTP 200 + clean teardown), rich
`ops register` (full sos-lab runtime surface), `next` engine drives the
entire ship path (C17a golden replay), `--json` + exit codes, product
README + docs/{QUICKSTART,CONCEPTS,CLI}.md, openchamber project
registration step in BOOTSTRAP, secrets audit (tree+history clean),
release binaries built. Known open item (owner call, predates program):
1edge-mocks deployed-intent tag drift — a `ops build/deploy 1edge-mocks`
or a state acceptance heals it.

## Standing cautions

- Secrets: sos-lab `secrets/` are read by fleet only for key-name checks
  and the CF token (transient, never printed). The openchamber gh token
  once flashed into the chat transcript (key-filter slip) — chat-only,
  present in no file; rotate if that matters to you.
- `.vm/` is ~600 MB gitignored; `scripts/vm-tier/down.sh` for graceful
  ACPI shutdown.
- Journal `#` comment lines are the verify/incident channel (doctor and
  C5c/C6c skip them); WO-5's P4 predicate reads `# verify wo=<id>` lines.
- The fleet binary never runs an interpreter; the repo tree is
  interpreter-free repo-wide (C20d scans go/yaml/md/sh incl. tests/ and
  testdata/, from FLEET_ROOT). Goldens freeze the historical labctl
  formats — keep them green.

## Session close 2026-08-28 (WO-14..WO-20 + wrap)

| Item | State |
|---|---|
| Program | WO-14..WO-19 EXECUTED + WO-20 core (backup engine, mocks reconcile, safety rails, trio backup + restore drill, legacy purge) — all pushed (b85b625) |
| Backups | trio + secrets home in R2 hk-03-backup (restic 0.17.3); nightly 04:00 CronJobs (critical excluded); retention 7d/4w |
| Safety | quiesce_state.json crash-safety + sweep; registry critical:true quiesce refusal (openchamber); explicit-names-or---all scope; secrets home lost+recovered incident journaled |
| Legacy | python:3.12-alpine renderer REPLACED by fleet-built Go (dashboard-render:2026.08.28-r212549); dashboard-render.py deleted; C20d gate GREEN (no interpreted runtimes — repo/rendered/live); golden monitor.json flipped via FLEET_GOLDEN_REGEN |
| Agent env | interpreter deny LIVE: permission block in ~/.config/opencode/opencode.jsonc (mirrors BOOTSTRAP step 6c); corpus is interpreter-free — JSON shape asserts via tests/lib/jsonq (stdlib Go) |
| Corpus | 46u/577a fail=0 skip=0 (`FLEET_WO=WO-21 ./scripts/fleet verify`, journaled); check 6/6 |
| OPEN | (1) WO-21 (OPEN): C20c kill-test unit + RESTORE/GLOSSARY/PROJECTS-GUIDE docs; (2) OWNER: rotate R2 token + RESTIC_PASSWORD (transited chat) — `restic key add` then remove old; (3) OPENCHAMBER/OPENCODE session state NOT backed up (owner decision: rebuildable) |
| Secrets note | secrets home was deleted+recovered once (cause unidentified) — rotation (item 2) is strongly advised |
| Close-out (owner-directed) | interpreter-free workflow: C12b/C12d/C17a fixture edits are plain sed/awk, C12c/C17c shape asserts via tests/lib/jsonq; labfix renderer fixture deleted (C13b frozen sequence updated to the golden-flipped 14-call order); vm-tier seed server ported to stdlib Go; C20d gate hardened (scans run from FLEET_ROOT — were vacuous inside the corpus — and cover tests/ + testdata/); deny LIVE in user config; WO-20 closed truthfully (pieces 3-6 integrated, P5) with open items split to WO-21 (OPEN); ops backup now REFUSES unknown service names (was a silent `BACKUP OK services=0`); corpus 46u/577a fail=0 + check 6/6 measured |
