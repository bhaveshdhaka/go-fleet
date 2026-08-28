# WORKING-NOTES.md — agent-to-agent working instructions (EPHEMERAL)

> NOT permanent documentation. This file exists to hand working knowledge
> between agents during the WO-5..WO-10 build. DELETE it when WO-10 lands —
> anything worth keeping gets codified into AGENTS.md/README first.
> Written 2026-08-27 after WO-7. Facts below were measured, not assumed.

## Where the program is

WO-4..WO-9 all EXECUTED (Go core, enforcement, distribution, read-only
parity, mutations dual-run, site migration — hk-03-dev is fleet-managed
with secrets referenced at ../sos-lab/secrets). Next: **WO-10 arjun.hk**
(then delete this file). For WO-10: arjun.hk zone_id already in the site
registry domains; `ops register` + `ops deploy` self-reconcile DNS/tunnel;
fleet init/onboard provide the SDLC side; the prod-stage promote needs an
approval file with an allowed actor (see .fleet.yaml)

## Environment facts (each one cost time to discover)

- **This container IS a pod on hk-03-dev** (ns `sos-lab`, SA `openchamber`,
  cluster-admin). Ambient `kubectl` works via in-cluster config. That is
  WHY sos-lab has no explicit KUBECONFIG anywhere. fleet must not copy
  that pattern: site access is declared explicitly (`ops/SITES.yaml`), and
  the runner materializes a temp kubeconfig (tokenFile/ca pointers, no
  secret copying) and passes `KUBECONFIG` + `HOME` (temp) ONLY.
- **gh**: not on default PATH. Binary at `~/.local/bin/gh` (v2.98.0,
  checksum-verified install). Token: `~/.config/openchamber/github-auth.json`
  → `[0].accessToken`. Use as `GH_TOKEN=$(python3 -c ...)` per command —
  never echo it, never write it to any file. NOTE: one command in this
  session accidentally printed the token into the chat transcript (bad
  key-name filter). If you see it, ignore it; it exists nowhere else.
- **Shell cwd persists between tool calls.** `cd ../sos-lab` in one call
  leaves the next call in sos-lab. Always pass workdir explicitly.
  An empty `$VAR` + walk-up/relative path once appended a test component
  to the LIVE fleet registry (WO-4 incident, repaired+journaled). Drill
  rule: neutral cwd, FLEET_ROOT pinned to the scratch copy.
- sos-lab layout: `config/registry.yaml` (intent, schema v2, validated by
  labctl/registry.py), `state/{deployed,builds}.json` (truth),
  `secrets/*.env` (gitignored; `cloudflare.env` holds CF_API_TOKEN),
  `labctl/*.py` is the engine. Its kubectl is AMBIENT — do not copy.
- QEMU drill VM may be running (`.vm/run/qemu.pid`). Leave it unless told.

## How to work here (what actually kept me out of trouble)

- The corpus (`bash scripts/test.sh`) is the gate AND the drift alarm:
  it caught doc drift (C9d), stale hardcoded asserts, fixture schema
  errors, and a premature journal line. Run it after every change; never
  integrate on red; never journal a result before you have read the
  FLEET SUMMARY line yourself.
- Per-piece loop that works: implement → `gofmt -w cmd internal && go vet
  ./... && go test ./internal/...` → build (`ci/build-fleet.sh`) →
  targeted unit → full corpus → journal `# verify` line → mark piece
  `integrated: true` in the WO front-matter.
- **New help command ⇒ update AGENTS.md in the same piece.** C9d compares
  them and will turn the corpus red otherwise (it did, correctly).
- **Test scenarios must neutralize ALL copied workorders** (generic sed:
  status→EXECUTED, integrated→true) before driving specific ones — WO-6's
  new file broke WO-5's tests until this became generic.
- Parity diffs of live output: run both commands back-to-back; kubectl's
  AGE/RESTARTS columns round between runs and will show phantom diffs.

## Pitfalls I went in circles on (don't repeat)

1. `yamlQuoteOpen`/multiline scalars: sos-lab's registry uses multi-line
   double-quoted scalars with `\` continuations AND plain multi-line
   scalars. The mini parser needed a merge pass + plain-fold. If you hit
   "bad indent", suspect a scalar construct, not indentation logic.
2. Env-discipline tests: bash injects `_`, `PWD`, `SHLVL` into every child
   script. Your fake kubectl must allow those + KUBECONFIG (+ HOME if you
   pass one) and fail on ANYTHING else — that check has real teeth.
3. kubectl without HOME writes a `.kube/` cache into the CWD. The runner
   passes a temp HOME; delete any `.kube` you find in a repo.
4. sos-lab's `./lab status` DISCARDS the nodes table (reachability probe
   only) and uses python `print()` semantics (extra trailing newline after
   pods). Byte-parity requires replicating quirks, not improving them.
5. The `enabled` field check, `port` int range, `image|repo` requirement —
   mirror `registry.py` exactly; my first fixture was itself invalid and
   "broke" tests that were actually working.
6. One premature journal PASS (appended before reading the summary) —
   corrected with an explicit `# journal-correction` line. Append-only
   means you fix forward, in the open.

## Truthful overengineering assessment

- **mini-YAML parser (~250 lines, hand-rolled)**: justified by the
  stdlib-only/hermetic house rule, but it was the single biggest time sink
  (3 debug cycles) and it is the code I trust least. WO-8 needs registry
  WRITES: do NOT grow this into a general YAML emitter. Mutate via
  rendered/anchored edits the way fleet rewrites deployments.yaml, and
  validate by re-parsing.
- **Twin-repo parity drills**: heavyweight but the best bug-catchers in
  the repo (they found the walk-up hazard, the awk semantics gaps, and
  three scenario bugs). Keep the pattern.
- **Predicates P1-P6**: right-sized. Do not add predicates without a
  burned-agent story behind them.
- **Doctor parity in Go**: the "fold engine, not instance" decision makes
  this mandatory, but note the cost: every sos-lab formatting quirk is now
  a fleet contract. WO-8 must keep mutating through sos-lab-compatible
  semantics until WO-8 parity passes — then hk-03-dev becomes fleet data.
- Skipped/deferred honestly: gh visibility is private (owner decides);
  workorder `deferred` piece state was added for that and is journaled.

## WO-8 quick-start (CONSUMED — dual-run passed 2026-08-28)

See workorders/WO-8.md measured results + journal evidence lines. The
mutation engine lives in `internal/fleet/{labrender,labops,opsmutate,
pyfmt,jsonio}.go`; the parity oracle is frozen under
`internal/fleet/testdata/golden/`. For WO-9: `site init --from sos-lab`
should read ops/SITES.yaml + the sos-lab registry/state and emit
fleet-side site data; secrets stay untouched; the post-migration deploy +
rollback drill can reuse the canary pattern (git-initialized source dir,
`canary`-style registry block, snap/compare harness written from scratch
in /tmp).
