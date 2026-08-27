# WO-4 — Go core: cmd/fleet replaces the bash control-plane CLI

> **Status:** EXECUTED this session · Owner directive: "execute the first
> open piece in PLAN.md (WO-4: the Go core, cmd/fleet)" via the house
> process: one workorder → plan section → decomposed pieces → every piece's
> verify output appended to lifecycle/journal/events.log → integrate. The
> corpus is the gate — never integrate on red.

## Plan section (owner decisions, dated 2026-08-27)

- Journaling: piece verify output appended to events.log as `#` comment
  lines (`# verify wo=... piece=... cmd=... fail=0 ...`). Doctor regex,
  C5c/C6c and every journal reader skip `#` lines — zero contract change
  now; WO-5 (P4 unjournaled-verify predicate) formalizes verify events.
- Integration shape: `scripts/fleet` becomes a thin deterministic shim that
  builds `cmd/fleet` hermetically (pinned go, GOPROXY=off, -trimpath,
  byte-reproducible) into gitignored `dist/fleet` when missing/stale, then
  execs it. `./scripts/fleet` path, AGENTS.md rule 1 and the corpus stay
  intact; the shell DAG corpus remains the black-box test spine.
- Scope split: `next` and `wo` ship minimal in WO-4 (file-state suggestion;
  list/show/new). Front-matter schema, predicates P1-P6 and the full
  guidance engine remain WO-5, exactly as PLAN.md assigns.
- Commit policy: one commit at WO-4 completion, repo-style message.

## Contract baselines (measured from bash, must be byte-reproduced)

    STATUS component=fleetctl kind=cli stage=prod
    STATUS component=fleethub kind=service stage=built
    STATUS SUMMARY components=2
    DOCTOR OK checked_components=2 issues=0

Promote/approve lines per ci/promote.sh + scripts/fleet sources (REFUSED ::
on stderr rc=1, ALREADY AT, PROMOTED component=... from=... to=... at=...,
APPROVED component=... stage=... file=...). FLEET_ACTOR defaults to agent.
FLEET_ROOT resolution mirrors fleethub: env override, then walk-up discovery
of the ops/ tree. Ambient KUBECONFIG is never read (AGENTS.md rule 7); WO-4
touches no cluster at all.

## Decomposed pieces (each: verify → journal → integrate; corpus green required)

1. Module scaffold + hermetic build — root go.mod
   (github.com/bhaveshdhaka/go-fleet, go 1.27.0 pin), cmd/fleet dispatch,
   internal packages, embed.FS templates; build flags mirror block 03.
   verify: go vet ./... offline; build twice → tree_hash byte-equal;
   `bash scripts/test.sh` fail=0.
2. File-contract layer — stdlib-only parsers/writers for registry,
   environments, state, gates, approvals, journal (append-only, exact
   awk-equivalent semantics). verify: go test ./... + corpus fail=0.
3. status/doctor/registry-check/approve/promote parity — machine contract
   lines byte-identical; promote re-runs gate units via
   `bash scripts/test.sh <units>`; state rewrite + one journal line;
   idempotent repeats. verify: C9b scratch-copy suite + corpus fail=0.
4. init/onboard/next/wo/verify (minimal) + templates — init scaffolds the
   SDLC skeleton from embed.FS; onboard = registry entry + pipeline file
   (doctor must go ALL CLEAR); wo list/show/new; next suggestion;
   verify journals a `# verify` line itself. verify: C9c + corpus fail=0.
5. Integration — scripts/fleet shim, ci/promote.sh shim (keeps C5c green),
   AGENTS.md/README command tables, C5d doc-sync update, new units
   C9a_go_core_build / C9b_cli_contract / C9c_sdlc_commands / C9d_doc_sync.
   verify: full corpus fail=0 (C9 units green + 17 existing units green)
   + ./fleet doctor ALL CLEAR. Only then EXECUTED.

## Standing guardrails

- QEMU drill VM (pid in .vm/run/qemu.pid) left running; sos-lab stays
  authoritative for hk-03-dev until WO-8 parity — WO-4 performs zero
  cluster operations and never reads ambient KUBECONFIG.
- Secrets never enter repo files. dist/ and .tmp/ are gitignored.

## Measured results (all verified this session, journaled as # verify lines)

| Piece | Verify | Result |
|---|---|---|
| 1 scaffold + build | vet offline; build x2; corpus | byte-identical sha256 2af611c5…; fleet 1.27.0; 17u/176a fail=0 |
| 2 contract layer | go test ./internal/fleet; corpus | ok; 17u/176a fail=0 |
| 3 CLI parity | twin-repo 40-line scenario drill; corpus | transcript+state+journal+approvals identical; 17u/176a fail=0 |
| 4 SDLC surface | scratch smoke (init/onboard/next/wo/verify); corpus | all green; 17u/176a fail=0 |
| 5 integration | C9a–C9d targeted; FULL corpus | C9 pass=71; 21u/257a fail=0 skip=0 |

Incident (one, repaired, journaled): a smoke drill ran with an empty
FLEET_ROOT from the live repo as cwd; walk-up resolved the live repo and
`onboard demo` appended one registry block to live ops/PROJECTS.yaml
(state/journal/approvals untouched). Restored via git checkout; doctor ALL
CLEAR re-verified. Lesson: drills always run from a neutral cwd with
FLEET_ROOT pinned to the scratch copy; the shims pin FLEET_ROOT for the
binary so the product never resolves the repo by walking up.

Delivered: go.mod (module github.com/bhaveshdhaka/go-fleet, go 1.27.0),
cmd/fleet, internal/fleet (parsers byte-faithful to the bash awk semantics,
writers, all ten commands), internal/fleet/templates + embed.FS,
ci/build-fleet.sh (hermetic deterministic builder), thin shims
scripts/fleet + ci/promote.sh, AGENTS.md/README sync, C5d retargeted to the
Go core, new units C9a/C9b/C9c/C9d. Full corpus: 21 units / 257 assertions,
fail=0 skip=0. ./fleet doctor ALL CLEAR.
