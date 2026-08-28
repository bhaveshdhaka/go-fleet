# PLAN.md — go-fleet product program

status: ACCEPTED
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
- 2026-08-28 Owner directive: "no more sos lab, only go fleet" — completion
  program WO-14..WO-19 (this ladder). Human-in-loop = scope approval per
  workorder (start), prod approvals (runtime), delivery acceptance (end);
  agents run everything between via the `next` engine.
- 2026-08-28 Secrets home: ONE mechanism — `$FLEET_SECRETS_HOME/<site>/`
  when the env seam is set, else `$HOME/.fleet/secrets/<site>/`. The
  `secrets_dir:` SITES.yaml override is DELETED (no predecessor-tree
  references ever again); `site init --from` copies predecessor secrets
  into the secrets home. Secret values never enter git/journal/logs/chat.
- 2026-08-28 MCP server CUT: the binary contract (next engine, machine
  summary lines, --json, documented exit codes) is the agent determinism
  guarantee; an MCP wrapper adds surface without adding determinism.
  Revisit only on demonstrated friction in the owner's agent workflow.
- 2026-08-28 openchamber integration lives in BOOTSTRAP.md only (register
  the fleet project + seed the first agent session); the fleet binary
  stays openchamber-independent (public-product safety).

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

Completion program (owner directive 2026-08-28, WO-11..WO-13 docs/public
already executed):

- WO-14 Secrets divorce: secrets home (one mechanism, outside the repo);
  `secrets_dir` override deleted; site init copies predecessor secrets;
  hk-03-dev migrated off ../sos-lab/secrets; BOOTSTRAP.md CF token STORED
  in the secrets home (transient-only rule deleted).
  verify: bash scripts/test.sh + ./scripts/fleet check + live ops doctor
- WO-15 Fresh-install path: `site new` (scaffold, dry-run byte-equality),
  `site tunnel create` (CF API), `infra deploy` (registry/gatus/dashboard/
  cloudflared as built-ins), `site canary` (build→deploy→verify→monitor→
  remove PASS line); one QEMU fresh-site scenario in test-onvm.sh.
  verify: corpus + VM drill evidence
- WO-16 Rich register + determinism core: ops register exposes
  storage/mounts/resources/probePath/runAsUser/args/serviceAccount/
  dockerfile; `next` engine sequences the full ship path (wo new → onboard
  → verify → promote dev → ops build → ops deploy → ops verify → prod
  refusal until approval); golden replay corpus unit of the whole path.
  verify: corpus (replay unit included)
- WO-17 Agentic I/O: --json on read verbs + next; exit codes 0/1/2/3
  documented and asserted; mutation summary lines byte-identical.
  verify: corpus JSON goldens + exit-code unit
- WO-18 Onboarding docs: README product rewrite (feature tour framed as
  problems solved, comparisons, quickstart), docs/QUICKSTART+CONCEPTS+CLI;
  BOOTSTRAP.md registers the fleet openchamber project + seeds the first
  agent session; `fleet` no-args guided tour.
  verify: C9d/C5d doc sync + corpus
- WO-19 Launch: secrets audit sweep (journal/registry/git history) +
  release binaries + SHA256SUMS + acceptance checklist; owner flips the
  final switch (repo already public).
  verify: audit script + corpus + checklist

## State

- **Completion program WO-14..WO-19 EXECUTED 2026-08-28** (owner
  directive "no more sos lab, only go fleet"): secrets divorce (one
  mechanism, outside the repo; BOOTSTRAP token STORED), fresh-install
  path with LIVE CANARY PASS on hk-03-dev (kaniko build sha-pinned,
  public HTTP 200, clean teardown, DNS/tunnel reconciled), rich
  `ops register` (full runtime surface), `next` engine over the full
  ship path with a golden replay unit (C17a), `--json` + documented
  exit codes (C17c), product README + docs/{QUICKSTART,CONCEPTS,CLI}.md,
  BOOTSTRAP openchamber project registration + first-agent-session
  seeding, secrets audit (tree+history clean, C19a), release binaries.
  Corpus 44 units / 550 asserts fail=0; check 6/6. Awaiting owner
  acceptance of the delivery.
