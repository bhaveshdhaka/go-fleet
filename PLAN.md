# PLAN.md — go-fleet product program

status: ACTIVE
module: github.com/bhaveshdhaka/go-fleet
license: MIT
owner: bhavesh

## Objective

Turn the fleet lab package into a free, standalone, installable product:
one static Go binary that enforces a deterministic SDLC for agent-driven
development — plan → small pieces → journaled verification → integration —
with self-correcting guidance (every violation prints its exact fix
command). File-state predicates enforced by the binary; markdown files are
data the binary validates, never prompts hoping an LLM behaves.

## Decisions (immutable, dated)

- 2026-08-27 Fold engine, not instance: generic ops logic (the `./lab`
  verbs) moves INTO fleet as Go; hk-03-dev specifics become site DATA only
  after dual-run parity. sos-lab stays authoritative until WO-8 passes.
- 2026-08-27 Enforcement policy: lifecycle mutations hard-blocked by gates;
  authoring drift reported with fix commands (doctor/next), not blocking.
- 2026-08-27 Human-in-loop: ONLY at scope/plan approval (start) and prod
  acceptance (end). Middle hops auto-approve as FLEET_ACTOR=agent.
- 2026-08-27 Module path github.com/bhaveshdhaka/go-fleet; repo created at
  WO-6 via gh. CLI is Go (cmd/fleet); shell DAG corpus stays as the
  deterministic black-box test spine; blocks 01-04 stay thin bash.
- 2026-08-27 arjun-hk is the first real customer, built THROUGH the product
  (WO-10), deployed via fleet ops to hk-03-dev at arjun.hk.

## Pieces (each = one workorder; verify command must pass before integrate)

- WO-4 Go core: cmd/fleet replaces bash scripts/fleet —
  init/onboard/status/doctor/next/wo/approve/promote/verify, same file
  contracts, embed.FS templates.
  verify: bash scripts/test.sh   (C9 units green + full corpus)
- WO-5 Enforcement v1: workorder front-matter schema; predicates P1-P6
  (dirty-tree-without-WO, missing plan link, missing decomposition,
  unjournaled verify, unintegrated pieces, journal tamper); fleet next
  guidance engine; approval-actor policy in .fleet.yaml.
  verify: bash scripts/test.sh   (C10 units green)
- WO-6 Distribution: MIT LICENSE; version stamp; static release build
  (linux/darwin) + SHA256SUMS; gh repo create bhaveshdhaka/go-fleet;
  installer path in install.sh.
  verify: fleet version + checksum verify unit + corpus
- WO-7 Ops engine (read-only parity): site model + registry schema;
  fleet ops status/doctor matching ./lab output against live hk-03-dev.
  ZERO mutations in this piece.
  verify: parity diff vs ./lab + corpus
- WO-8 Ops engine (mutations): deploy/build/dns/monitor via fleet;
  DUAL-RUN with ./lab on hk-03-dev until identical results.
  verify: identical both paths + ./lab doctor ALL CLEAR
- WO-9 Site migration: fleet site init --from sos-lab; hk-03-dev becomes a
  fleet-managed site; operational history archived; secrets untouched.
  verify: post-migration deploy + rollback drill
- WO-10 arjun-hk end-to-end: fleet init arjun-hk; single-page Go site
  (dollarbucks menu, iOS/Safari/retina contract tests); gated promote;
  fleet-ops deploy to hk-03-dev; DNS arjun.hk; monitoring.
  verify: master checklist completion list, all items evidenced

## State

- Done through WO-5: see HANDOVER.md (corpus 25 units / 292 asserts green;
  enforcement v1 live: front-matter schema, P1-P6, full next engine,
  actor policy).
- Next action: execute WO-6 (distribution, first open piece).
