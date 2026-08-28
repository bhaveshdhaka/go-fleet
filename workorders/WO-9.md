---
wo: WO-9
title: Site migration — hk-03-dev becomes a fleet-managed site
status: EXECUTED
plan: PLAN.md
pieces:
  - id: 1
    title: site init --from (registry/state/templates import, history archive, SITES.yaml cutover, secrets_dir reference) + site model extensions + C14a
    verify: go vet + go test + C14a + full corpus
    integrated: true
  - id: 2
    title: ops register verb (lab add_service parity) + C14b offline drill on a fleet-managed site
    verify: C14b + go vet + go test + full corpus
    integrated: true
  - id: 3
    title: LIVE migration of hk-03-dev + post-migration deploy/rollback drill (canary via fleet only)
    verify: journaled # verify lines + both doctors green + registry byte-baseline after drill
    integrated: true
  - id: 4
    title: integrate (corpus green, check 6/6, HANDOVER/PLAN/AGENTS updated, commit+push)
    verify: bash scripts/test.sh + ./scripts/fleet check
    integrated: true
---

# WO-9 — Site migration: `fleet site init --from sos-lab`

> **Status:** IN PROGRESS this session · Owner directive: goal message
> 2026-08-27 (WO-8 → WO-9 → WO-10). WO-8 dual-run PASSED (identical
> results), so the cutover precondition holds: sos-lab stays authoritative
> UNTIL this migration; afterwards hk-03-dev is fleet-managed and sos-lab
> becomes the archived predecessor.

## Plan section (decisions, dated 2026-08-28)

- **Site data moves into fleet's git; secrets do not.**
  `fleet site init <name> --from <lab_root>` creates
  `ops/sites/<name>/{config/registry.yaml, state/*.json, templates/}` —
  the SAME file contracts the engine already reads — and copies the
  current state snapshot into `ops/sites/<name>/archive/` with a
  MIGRATION manifest (timestamp, source path, measured facts). Secret
  files are NEVER copied or moved: SITES.yaml gains an optional
  `secrets_dir:` field and the migrated site points it at the
  predecessor's gitignored `secrets/` (`../sos-lab/secrets`). "Secrets
  untouched" is enforced by a C14a grep: no secret VALUE may exist
  anywhere under the fleet tree after migration.
- **Cutover semantics.** The site entry flips to `engine: fleet` with
  `lab_root: ops/sites/<name>`. The ops verbs accept engine `fleet` —
  they already speak exactly these file contracts, so no behavior fork:
  same LabView, same runner, same mutation verbs, now rooted at fleet's
  own data (state writes become git-tracked operational history — the
  archive freezes the as-imported snapshot). sos-lab's repo is left
  byte-identical to its post-WO-8 baseline and is from then on an
  archive: its `./lab doctor` keeps reporting on its own (now frozen)
  data and is no longer the arbiter.
- **Repo-relative build paths.** kaniko contexts must live under the
  `/workspace` hostPath. Repo resolution changes from
  `dirname(lab_root)` to `dirname(FLEET_ROOT)` (the workspace root) —
  identical behavior for the sos-lab layout (sibling dirs) and correct
  for the fleet site (`ops/sites/hk-03-dev` is deep inside the repo).
  Overlay contexts are computed as `/workspace/` + relative path from
  the workspace root, which reproduces lab's hardcoded
  `/workspace/sos-lab/images/...` byte-for-byte for the old site.
- **`ops register <name>`** (lab `add_service` parity): flags
  `--host --port [--namespace] [--image --repo --secret --env]` write a
  canonical service block (2-space style, `enabled: false` default) at
  the end of the site registry + re-parse validation. WO-9's drill needs
  a target and WO-10 needs onboarding; `ops remove --unregister` is its
  inverse and restores byte-baseline.
- **Post-migration drill = fleet-only.** No dual-run: sos-lab is no
  longer authoritative. Canary pattern again (git-initialized source,
  two builds v1→v2 so rollback has somewhere to go): register → build →
  deploy → verify HTTP 200 → build v2 → deploy → verify → rollback →
  verify image reverted + state record → `ops remove --unregister` →
  site registry back to byte-baseline. `fleet ops doctor` all clear
  throughout; `./lab doctor` (predecessor) expected to still report on
  its frozen baseline — documented, not a failure.
- **Rule 7 unchanged**; neutral cwd + explicit FLEET_ROOT for drills;
  no secret values in repo, journal, or chat.

## Pieces (each: verify → journal → integrate; corpus green required)

1. **Site migration engine**: Site model `secrets_dir` + engine `fleet`
   acceptance; `cmdSiteInit` (import, archive+manifest, anchored
   SITES.yaml cutover, refusals: existing target, invalid source,
   re-init); workspace-root repo/overlay path resolution.
   **C14a**: full offline migration of a fixture lab — structure,
   byte-preserved registry, archive manifest, SITES.yaml cutover,
   secret-value grep (must be absent), ops status works on the migrated
   site, re-init refuses.
2. **`ops register`** + **C14b**: offline drill against a fleet-managed
   fixture site — register (canonical block + validation refusals),
   deploy via fake kubectl with state writes landing in the SITE dir,
   remove cleanup; assert the predecessor lab fixture is untouched.
3. **LIVE migration + drill**: `fleet site init hk-03-dev --from
   ../sos-lab`; commit migration; then fleet-only canary drill
   (register/build v1/deploy/verify/build v2/deploy/verify/rollback/
   verify/remove) with journaled evidence; doctors green; registry
   byte-baseline restored; leftover Jobs/CNAME documented as in WO-8.
4. **Integrate**: corpus fail=0, check 6/6, AGENTS.md/help sync,
   HANDOVER/PLAN updated, WORKING-NOTES consumed, commit+push.

## Standing guardrails

- Mutations through ./fleet only (site init, ops register/ops verbs);
  secrets never copied/moved/printed; neutral cwd + FLEET_ROOT in drills.
- Stop-and-report on any refusal, red corpus, or ambiguous contract.
- sos-lab tree must end byte-identical to its post-WO-8 baseline.

## Measured results

| Piece | Verify | Result |
|---|---|---|
| 1 site init --from | go vet + go test + C14a + corpus | **PASS** — C14a (20 asserts): registry byte-preserved, archive+MIGRATION manifest, SITES.yaml cutover (append + in-place flip), re-init/corrupt-source refusals with no dir residue, secret-VALUE grep teeth (zero values under fleet tree); corpus 35u/432a fail=0 |
| 2 ops register | C14b + go vet + go test + corpus | **PASS** — C14b (20 asserts): canonical block append + refusals (duplicate/bad port/image-or-repo), deploy state lands in SITE dir, remove restores post-migration byte-baseline, predecessor lab data untouched. In-cycle fixes: labServiceBlock absent-name bug (named return masked it — masked refusals were refusals for the wrong reason), secrets_dir override honored by slug/CF-token paths, state fixtures normalized to sorted-key lab format |
| 4 integrate | corpus + check | **PASS** — C12a made structural (machine contract, not live data) + engine-vocabulary teeth; C12b/c/d scratch rewrites normalized (site-entry-anchored, secrets_dir stripped in fixtures); corpus 35u/433a fail=0, check 6/6 |
| 3 LIVE migration + drill | journaled evidence + doctors | **PASS — hk-03-dev IS FLEET-MANAGED.** `site init` cutover (engine: fleet, lab_root ops/sites/hk-03-dev, secrets_dir reference, archive written). Fleet-only drill: register → build v1 → deploy → HTTP 200 + content verified → build v2 → deploy → HTTP 200 + v2 routed immediately (deviation #3) → rollback → v1 image+content, state rolled_back=true → remove → registry byte-baseline. Doctor all clear throughout (one CF-propagation-lag FAIL right after remove's PUT, resolved on re-run, journaled). Leftovers: 4 completed build Jobs + 1 CNAME (inert) |
