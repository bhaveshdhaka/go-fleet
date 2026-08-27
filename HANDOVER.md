# fleet — engineering handover

Measured facts as of this session (nothing inferred):

## Committed tree
master @ 5e43e61 = C0 harness + C1a/b/c + C2a packages.

## Working tree (intentionally UNCOMMITTED pending owner sign-off)
- C3 package: apps/fleetctl, scripts/blocks/03-pipeline.sh, tests/C3a..C3c
- Block-02 fixes surfaced by tests/C1d_toolchain_idempotent:
  bin_present v-strip compare bug; pin(tailwindcss) mapping; kubectl probe
  rewritten for `--output json`; templ os/arch unbound crash; plus per-tool
  install verification and a deterministic failed_tools summary line.
- C4 package: infra/k8s manifests x3, scripts/blocks/04-deploy.sh, C4a.
- C5 package: SDLC spine — ops registry/state/runbooks, lifecycle gates +
  journal, ci/promote.sh gate engine, scripts/fleet CLI, AGENTS.md,
  workorders/WO-1.md, tests C5a..C5d.
- C6 package: apps/fleethub dashboard+approver (stdlib-only), tests C6a..C6c,
  workorders/WO-2.md.
- install.sh bootstrap with FLEET_INSTALL_OK/FAIL machine contract
  (--verify runs full corpus; no sudo; repo-local .toolchain).
- README.md rewritten: plain-English + geek/agent parts incl. the SDLC story.

## Verified this session
- Full corpus before C5/C6: units_run=9 pass=98 fail=0 skip=0.
- ./fleet doctor: ALL CLEAR after C6 landed (fleethub dir + C6c unit exist).
- promote.sh contract smokes: dry-run byte-stable; illegal/backwards refused;
  approval-missing refuses naming exact path; repeat = ALREADY AT, zero writes;
  gate units are re-executed at promotion time inside isolated repo copies.
- fleethub E2E: unknown component 400; valid dev approval 201 with correct
  file content; duplicate idempotent (200 already, journal unchanged);
  journal line format identical to CLI.
- install.sh agent smoke in minimal container: FLEET_INSTALL_OK,
  restic honestly skipped without bzip2, machine lines as contracted.

## Open for next session (agents read me FIRST)
1. Tier-1 VM drill script (scripts/test-onvm.sh) remains unbuilt — real k3s
   daemon + live apply + rollback drill on a disposable Ubuntu host.
2. Commit split awaiting owner: fix-block / C3 / C4 / C5 / C6(+docs).
3. Possible future: registry-driven manifest templating once more than one
   real service ships through prod.
