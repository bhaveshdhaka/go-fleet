---
wo: WO-8
title: Ops engine (mutations) — deploy/build/dns/monitor via fleet, dual-run with ./lab
status: EXECUTED
plan: PLAN.md
pieces:
  - id: 1
    title: lab mutation engine in Go (renderers, emitters, state writers, registry editor) + C13a render parity
    verify: go vet + go test ./internal/... + C13a + full corpus
    integrated: true
  - id: 2
    title: ops mutation verbs (build/deploy/rollback/dns/monitor/remove/verify) + AGENTS.md/help sync + C13b offline drill
    verify: C13b + go vet + go test + full corpus
    integrated: true
  - id: 3
    title: LIVE dual-run drill on hk-03-dev (canary through both engines) + teardown
    verify: journaled # verify lines with measured parity per mutation + ./lab doctor ALL CLEAR
    integrated: true
  - id: 4
    title: integrate (corpus green, check 6/6, HANDOVER/PLAN updated, commit+push)
    verify: bash scripts/test.sh + ./scripts/fleet check
    integrated: true
---

# WO-8 — Ops engine (mutations): fleet operates hk-03-dev alongside ./lab

> **Status:** IN PROGRESS this session · Owner directive: goal message
> 2026-08-27 (WO-8 → WO-9 → WO-10; WO-4..7 EXECUTED and pushed). This is
> the first workorder permitted to MUTATE the live cluster — every
> mutation goes alongside `./lab` (dual-run, identical results).

## Plan section (decisions, dated 2026-08-27)

- **Scope** = PLAN.md WO-8: `deploy/build/dns/monitor` via fleet, plus
  `rollback` (WO-9's verify needs it) and `verify`/`remove` (needed to
  prove and to leave the cluster clean). `remove` carries ONE documented
  deviation from labctl: it also deletes the service's
  `state/deployed.json` entry (and with `--unregister`, its
  `state/builds.json` entry) — measured sos-lab wart: `./lab remove`
  leaves doctor red forever (`deployed/<svc>` FAIL) because no tool ever
  cleans state. Everything else mirrors labctl semantics exactly.
- **Parity contract = result parity.** After every fleet mutation:
  (a) `./lab doctor` ALL CLEAR, (b) `./lab status` == `fleet ops status`
  byte-identical, (c) `./lab dns` (read-only) reports no drift, (d) the
  mutation's full effect (cluster objects, state files, Cloudflare
  records, monitor CM data) matches what `./lab` produces for the same
  input. kubectl's `last-applied-configuration` annotation is compared at
  the PARSED-object level (kubectl serializes it as JSON regardless of
  input format), so even the bookkeeping is provably identical.
- **Manifests are applied as JSON, not YAML** (WORKING-NOTES
  overengineering rule: do not grow miniyaml into a YAML emitter). Go
  renders the same object graphs labctl renders; `kubectl apply -f -`
  receives JSON; the applied live objects are identical. Proven against
  FROZEN GOLDENS (amended 2026-08-28, owner pushback: "why is python even
  being considered"): `internal/fleet/testdata/golden/*.json` were
  generated ONCE by the real labctl 2.0.0 from the testdata/labfix
  fixture registry covering every registry field (storage, mounts,
  hostPath+initContainer, env/envFrom, args, runAsUser,
  serviceAccount+CRB, probePath, resources override, overlay builds).
  Go tests + corpus C13a are PURE GO — no python, no sibling dependency,
  no skip paths. The live python-vs-fleet comparison was run once during
  development (all fixtures + monitor + tunnel parity PASS) and is
  repeated as a journaled one-off at the piece-3 dual-run; python is the
  frozen ORACLE, never a runtime or test-time dependency of fleet.
- **Two string formats need bounded emitters** (used inside ConfigMap
  data): `pyYamlDump` for gatus `config.yaml` (pyyaml
  safe_dump sort_keys=False of the endpoints list) and `pyJSONDumps` for
  state/principles/hosts JSON strings (python json.dumps default
  separators `, ` / `: `, sort_keys). Both are ~50-line bounded emitters
  asserted byte-identical against python output in C13a — NOT general
  emitters.
- **State writes byte-format-identical** to labctl `state.py`:
  `json.dump(indent=2, sort_keys=True)` + trailing newline, tmp+rename
  atomic, `flock` on the same `state/.<name>.lock` files, entry field
  sets identical (record_deploy / record_build / record_rollback incl.
  `git_sha` null-vs-absent asymmetry, `node` from `SOS_LAB_NODE` env with
  hk-03-dev default, UTC `%Y-%m-%dT%H:%M:%SZ`).
- **Registry writes = surgical anchored edits** (flip `enabled:`, delete
  a service block), validated by re-parsing with the WO-7 mini parser +
  `validateLabRegistry`, atomic tmp+rename. Never a whole-file rewrite
  (that is lab's style and needs a YAML emitter).
- **Secrets discipline unchanged**: `ensure_secret` hands the env-file
  PATH to kubectl (`--from-env-file`) — values never enter fleet memory;
  `check_secrets` reads key names only; CF token transient. Canary
  secret values are random tokens created and deleted with the canary.
- **Dual-run discipline (hard rule)**: mutations ONLY alongside `./lab`.
  Precondition for every fleet mutation: `./lab doctor` ALL CLEAR. The
  canary is deployed by `./lab` FIRST (authoritative engine), snapshotted,
  then operated by fleet and snapshotted again — both engines must
  produce identical results for identical inputs.
- **Rule 7 unchanged**: every kubectl call goes through the explicit
  runner (temp kubeconfig, child env = KUBECONFIG + temp HOME only,
  C12d teeth). Ambient KUBECONFIG exists in this pod's env and is
  never consulted.
- **Neutral cwd + explicit FLEET_ROOT** for every drill; live parity
  runs back-to-back (kubectl AGE/RESTARTS round between runs — compare
  immediately).

## Pieces (each: verify → journal → integrate; corpus green required)

1. **Lab mutation engine in Go** (`internal/fleet/labops.go` +
   `labrender.go` + `pyfmt.go` + `jsonio.go`): render_service /
   render_kaniko_job / render_monitor_cms / render_dashboard object
   graphs; tunnel ingress list; gatus config string; pyJSON strings; lab
   state writers (deployed/builds, flock+atomic, byte-format parity);
   registry anchored edits (flip enabled, remove service block,
   re-parse-validated); cloudflare write calls (ensure_cname,
   put_tunnel_config). Go unit tests + **frozen goldens** (labctl
   2.0.0-generated, committed under testdata/golden): **C13a** corpus
   unit runs the pure-Go parity suite.
2. **Ops mutation verbs** wired into `cmd/fleet` dispatch with
   lab-identical output contracts (`BUILT`, `DEPLOYED`, `MONITOR OK`,
   `dns:`/`drift:`, `rolled back`, `removed`, verify `-> HTTP`): `ops
   build|deploy|rollback|dns|monitor|remove|verify`. AGENTS.md + help
   text updated in the SAME piece (C9d). **C13b**: offline mutation
   drill — fake kubectl with C12d env-teeth records every applied doc;
   asserts deploy two-phase secret-check, apply order (secret, docs,
   rollout, dns, monitor), state-file bytes, registry flip, machine
   lines, rollback state record, dns read-only vs --apply shapes, remove
   state cleanup.
3. **LIVE dual-run on hk-03-dev** (journaled per mutation, zero repo
   artifacts beyond evidence lines):
   canary prep (`canary-src` sibling + registry block `canary`
   (repo/host/port 8080/probePath /healthz/resources override/storage
   1Gi/secrets [CANARY_TOKEN]/env) + `secrets/canary.env` random token)
   → `./lab build canary` + `./lab deploy canary` → snapshot S1 →
   `fleet ops deploy canary` (re-apply identical docs, idempotent) → S2
   → `fleet ops build canary` (tag T2) → `fleet ops deploy canary` → S3
   → `fleet ops rollback canary` (back to T1) → `fleet ops dns --apply`
   (no drift) vs `./lab dns` → `fleet ops monitor` CM-data parity vs
   `./lab monitor` → `./lab verify canary` + `fleet ops verify canary`
   → teardown `fleet ops remove canary --delete-data --unregister` +
   remove canary-src + secrets/canary.env → registry byte-baseline
   restored, both doctors green. Snapshot compare in /tmp/opencode only
   (normalize timestamps/tags; Secret objects compared by key names
   only, values never).
4. **Integrate**: full corpus fail=0, `fleet check` 6/6, WO-8 EXECUTED
   with measured results table, HANDOVER.md + PLAN.md state updated,
   WORKING-NOTES.md WO-8 section pruned, single commit, push.

## Standing guardrails

- Mutations alongside ./lab only; stop-and-report on any refusal, red
  corpus, or ambiguous contract (no improvisation).
- Never touch existing real services (openchamber/1edge/trio/platform)
  beyond what dual-run parity proves identical; no `--force`.
- Secrets never in repo files, journal, or chat; VM left running;
  sos-lab git tree clean at WO-8 end (canary intent restored).
- gh not needed this WO; push via existing remote when green.

## Measured results

| Piece | Verify | Result |
|---|---|---|
| 1 lab mutation engine | go vet + go test + C13a + corpus | **PASS** — python-parity on first principled run: state bytes + gatus string byte-identical to labctl 2.0.0; render parity green after 3 in-cycle fixes (nil-slice vs `[]`, kubeconfig-src basename uses container path, kaniko ns arg); registry editor refuses unknown/absent; corpus 32u/363a fail=0. Oracle frozen as committed goldens per owner pushback (see `# decision` journal line) |
| 2 ops mutation verbs | C13b + go vet + go test + corpus | **PASS** — C13b (29 asserts): deploy call-sequence byte-matches lab order (secret create→apply→dep→svc→rollout→6 monitor docs→gatus template→rollouts), state-file bytes, registry flip, rollback record, remove state cleanup, two-phase secret refusal, verify refusal, rule-7 env teeth; CF writes shape-asserted via httptest; AGENTS.md/help synced (C9d green); corpus 33u/392a fail=0, check 6/6 |
| 3 LIVE dual-run | journaled per mutation + ./lab doctor ALL CLEAR | **PASS — DUAL-RUN COMPLETE.** Canary `canary` (busybox httpd, repo build) went through BOTH engines on live hk-03-dev: lab build+deploy first (S1), then `fleet ops deploy` (S1==S2 **byte-identical** normalized snapshots), `fleet ops build` T2 + deploy (S3==S1 modulo the git sha), `fleet ops rollback` (image reverted, lab-format state), `fleet ops dns` ±`--apply` (line-identical), `fleet ops monitor` (**CM data byte-identical** vs `./lab monitor`), `fleet ops verify` HTTP 200. After EVERY fleet mutation: `./lab doctor` all clear + `./lab status`==`fleet ops status` byte-identical. Teardown via `fleet ops remove` restored the pre-drill baseline (state==baseline, registry restored, both doctors all clear). Three in-cycle discoveries journaled: pyyaml-style registry reformat (parser fixed + regression test), lab `./lab status` null-sha crash (sos-lab bug, avoided via git init), deploy's pre-flip tunnel PUT semantics (mirrored bug-for-bug; remove's stale-PUT fixed — journaled deviation #2). Leftovers by lab design: 2 completed build Jobs + 1 CNAME |
