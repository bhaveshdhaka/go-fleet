# HANDOVER — go-fleet program

> Read order for any agent arriving cold: `README.md` → `AGENTS.md` (the
> law) → `PLAN.md` (the program) → this file. Then `git log --oneline -8`.
> Facts below are measured, not inferred.

## State at a glance (2026-08-29 — fresh-server era, estate LIVE)

| Item | State |
|---|---|
| Box | hk-03-dev: fresh Ubuntu 24.04.3 LTS, x86_64, node IP 202.73.4.149; root SSH; opencode TUI + in-cluster openchamber UI |
| Repo | `/home/openchamber/workspaces/fleet` (moved 2026-08-30 from /root/workspaces — kaniko renders dirname(FLEET_ROOT), /workspace symlink removed); master `cde720b`+ cutover commit, pushed; remote github.com/bhaveshdhaka/go-fleet (public) |
| Corpus | 46u **fail=0** (574 pass, skip=1 — C4a kubectl tier skips honestly on offline hosts, passes on cluster hosts); `fleet check` 6/6; `ops doctor` ALL CLEAR (1 benign warning) |
| Cluster | k3s v1.36.3+k3s1 single node (namespace **`fleet`** — the sos-lab namespace is DELETED); `/etc/rancher/k3s/registries.yaml` HTTP mirror + `/etc/hosts` ClusterIP entry for `docker-registry.fleet.svc.cluster.local`; `/workspace` → symlink → `/root/workspaces` |
| Tunnel | **hk-03-dev-tunnel** (`50e0d13b-…`), config_src cloudflare, ingress reconciled from the registry (8 hosts); cloudflared runs IN-CLUSTER; sos-lab tunnel abandoned (exists in CF, unreferenced); bootstrap tunnel 472ae004 DELETED; host cloudflared uninstalled |
| Estate | ALL LIVE + verified: `aio.bhavesh.hk` 200, `dav.bhavesh.hk` 302, `usenet.bhavesh.hk` 302, `arjun.hk` 200, `1ed.ge` 200, `mock.1ed.ge` 200, `oc.bhavesh.hk` 200, `dashboard.bhavesh.hk/f178b53074aa2765/` 200 (slug-hidden BY DESIGN — `/` returns 404) |
| Data | trio + 1edge + 1edge-mocks restored from R2 `hk-03-backup/hk-03-dev` snapshot=latest (RESTORE.md §8 contract lines all seen); restic password PROVEN (snapshot listing rc=0); openchamber NOT restored (fresh, owner decision) |
| openchamber | **ON THE HOST** (cutover 2026-08-30, runbook EXECUTED — `plans/openchamber-host-cutover.md` in the pod PVC): user-level systemd unit as root (HOME=/home/openchamber, OPENCHAMBER_UI_PASSWORD set, foreground + OPENCHAMBER_SYSTEMD_UNIT → **update button ARMED**), `@openchamber/web` 1.21.1 + opencode-ai from npm (/usr prefix — unit ExecStart=/usr/bin/openchamber). Data migrated PVC→/home/openchamber (sessions db 321M, auth, passkeys, settings, projects carry over). Pod REMOVED from cluster; registry entry KEPT disabled = rollback hatch (`ops deploy openchamber` + `git checkout -- templates/cloudflared.yaml` + `infra deploy` restores it). Tunnel ingress oc → `http://202.73.4.149:3000` via CF API (fleet has no non-service target verb — journaled deviation). Doctor FAILs on openchamber parity + ingress-extra are the DESIGNED Day-0 state; Day-N (`ops remove --unregister`) clears them |
| Fleet ops from pod | WORKING: secrets home mounted (`/root/.fleet` → `/home/openchamber/.fleet`, uid 1000 owns), kubeconfig mounted at the SAME absolute path both sides (`/root/.kube/fleet.kubeconfig`, server = node IP), kubectl baked. SITES access: `kubeconfig:/root/.kube/fleet.kubeconfig` |
| fleethub | at **prod** (dev approval agent, prod approval owner-via-agent via UI session 09:18Z — owner-directed, journaled) |
| Fixes shipped today | 6 commits `cfdbb32..7655390`: corpus hermeticity ×6 (fixture gitignore, drills off-pod, harness PATH, C4a offline tier), `01-k3s.sh` +k3s1 tag, infra deploy namespace-ensure, TUNNEL_TOKEN→token secret mapping, `site tunnel create` zone-scoped account resolution, `LabRegistryHost(ns)` de-legacy, overlay Dockerfile committed intent, BOOTSTRAP.md rewritten with the measured runbook, `docs/MCP-BRIEF.md` |
| Next action | WO-22 phase 1 EXECUTED 2026-08-29: `fleet mcp` (stdio, read-only) corpus-green; phases 2–3 parked in `docs/MCP-BRIEF.md` |

## Rules that got agents burned (cumulative — now enforced/asserted)

- Ambient resolution in ANY form: kubectl creds (rule 7) AND repo-root
  walk-up. Fixes: site-declared access + constructed child env (C12d),
  shims pin FLEET_ROOT, drills from neutral cwd with FLEET_ROOT pinned.
- **Hermeticity is environment-shaped**: the corpus passed for months
  inside the old pod and broke on every fresh host (fixture gitignored
  by the bare `secrets/` pattern, drills assuming `KUBERNETES_SERVICE_HOST`,
  units resolving go from operator PATH, kubectl 1.36 phoning home even
  for `--dry-run=client`). Measured on a fresh box 2026-08-29 — all fixed.
- **k3s fork tags carry `+k3s1`**; bare Kubernetes tags 404 on get.k3s.io
  (01-k3s.sh fixed; test-onvm.sh:82 still has the bare form — untouched).
- **Zone-scoped CF tokens list NO /accounts** — resolve the account id
  from the zone object (fixed in `site tunnel create`; same lesson in
  BOOTSTRAP.md Step 5).
- **journal honesty**: never append a verify line before reading the
  summary; corrections/findings are `#` lines (append-only, in the open).
- **Secrets**: values only in the secrets home, never in git/logs/chat —
  the CF token + restic password + R2 keys transited chat on 2026-08-29;
  rotation is the owner's standing item.
- **pyyaml rewrite hazard (WO-8)**, **secrets-dir plumbing (WO-9)**,
  **labServiceBlock absent-name (WO-9)** — regressions asserted in corpus.
- **lab quirks mirrored bug-for-bug by design** (deploy PUTs tunnel config
  before the enabled flip; remove-side ones fleet fixes — journaled).

## Standing cautions (fresh-server additions in bold)

- **`/etc/rancher/k3s/registries.yaml` + the `/etc/hosts` ClusterIP entry
  are load-bearing**: remove either and EVERY service pod hits
  ImagePullBackOff. The kubeconfig file is uid-1000 owned with server =
  node IP; the openchamber image chmods `/root` 755 for the same reason.
  Do not "tidy" any of these.
- **The fleetboard is slug-hidden** (`dashboard.env` DASHBOARD_SLUG):
  `/` returning 404 is the design, not a fault.
- **openchamber rebuilds** go through `ops build` (base ~8 min, overlay
  after) — the overlay Dockerfile is committed intent; the pre-wipe copy
  was untracked and died with the old box. Do not re-add the `oc-tools`
  PVC mount: a fresh volume shadows `/home/openchamber/.npm-global`.
- **Two opencode instances, no auto-sync**: host TUI (`/root/.local/share
  /opencode`) and pod (PVCs). Provider key / config changes = seed both.
  The pod owns its opencode db on the PVC.
- **uid 1000 owns `/root/.fleet` + `/root/.kube/fleet.kubeconfig`** so
  pod sessions read them; host fleet ops run as root and are unaffected.
- Secrets: sos-lab-era files under `ops/sites/hk-03-dev/archive` are
  inert; live values only in the secrets home. `.vm/` ~600MB gitignored.
- The fleet binary never runs an interpreter; repo-wide interpreter-free
  gate (C20d) + live deny rules in BOTH opencode instances' configs.

## Next action

**WO-22 phases 2–3 remain PARKED** (owner scoped 2026-08-29: phase 1
only): mutation tools behind the actor policy + Tasks extension, then
Streamable HTTP + Cloudflare Access + registry listing — do not start
without an owner green-light. Phase 1 shipped: `fleet mcp` stdio
(official Go SDK v1.7.0, vendored — `-mod=vendor` now REQUIRED in every
main-module build path: build-fleet/build-release/C9a/C16b/C13a),
10 read-only tools + 5 contract resources, C22a (stdio contract) +
C22b (secret-leak guard) corpus units. Minor backlog: dashboard-render
build-vs-deployed state warning (bookkeeping), `ops status` cosmetic
"pods (sos-lab)" header, trio-as-openchamber-projects (owner declined
2026-08-29, revisit on ask).

## Session close 2026-08-29 (fresh server + estate bring-up)

- BOOTSTRAP.md executed end to end on a wiped box (first real run): every
  step's evidence captured in-session; the doc then rewritten with the
  measured sequence (supervisor split, token-verify + account-from-zone,
  never reconfigure shared tunnels, `--json` install contract, honest
  corpus skip, r2.env secrets-first, project mechanics, prod-gate
  expectation).
- RESTORE.md §3–§10 executed on the fresh cluster: secrets home restored
  (9 files), infra deployed, trio deployed + data restored, arjun-hk +
  1edge + mocks rebuilt from source and deployed, data restored, all
  hosts verified. Doctor ALL CLEAR.
- De-legacy completed under the owner directive: namespace `fleet`,
  tunnel `hk-03-dev-tunnel` (created via `site tunnel create` after the
  zone-scoped-account fix), `dashboard.bhavesh.hk`, project key
  `go-fleet`, registry host namespace-derived. sos-lab namespace deleted;
  sos-lab tunnel unreferenced.
- openchamber consolidated: in-cluster pod (registry-critical path),
  host copy retired; auth/config/projects seeded into PVCs; kubeconfig +
  secrets mounted; the "castrated pod" is not — pod sessions run the
  full fleet ops surface (measured).
- fleethub drove the whole ship path to prod (first session + owner via
  UI) — gates held where they must.
- Journal: findings in `#` lines 2026-08-29; commits `2f2597d`, `a323330`,
  `eb2ffb7`, `74818f6`, `7655390` pushed to master.

## WO-22 phase 1 close (2026-08-29, pod session)

- `fleet mcp` shipped read-only: official Go SDK v1.7.0 pinned + vendored
  (first external dep; `-mod=readonly` → `-mod=vendor` flipped in the
  five main-module build paths, fixture modules untouched — C9a
  byte-identical green). Adapter pattern: every tool self-execs the
  binary with `--json`, child env = explicit allowlist, FLEET_ROOT +
  cwd pinned (rule-7 discipline extends to MCP clients' arbitrary cwds).
- Surface: 10 tools (status/doctor/next/check/site list/wo list/wo show/
  ops status/ops doctor/ops dns report-only) + 5 resources (journal,
  PROJECTS/state/SITES/gates). No mutating verb reachable; `--apply`
  unreachable by construction. `doctor ok:false`/`cluster:false` are
  data, not errors; refusals = `isError` results.
- Lessons into the corpus: C22a (contract: exact tool set, structured
  content, honest errors, clean EOF exit) + C22b (canary value in the
  secrets home NEVER appears in any server output; negative control
  proves the canary). The SDK does NOT line-flush stdio — response
  bytes (sans trailing newline) appear incrementally; C22a/C22b poll by
  id and accept only jsonq-complete JSON (a bash `case *$'\n'*` line
  gate starves — measured, not theorized).
- C12d repaired for the de-legacy registry (fixture now pins the site
  to in-cluster access — its old premise died with the sos-lab-era
  SITES.yaml). Pod-only repair: `.toolchain/bin/{go,gofmt}` symlinks
  repointed to the in-pod src toolchain (gitignored; host unaffected).
- Corpus 48u **fail=0 skip=0** (600 assertions) incl. C22a/C22b; doctor
  ALL CLEAR; journaled `# verify ... wo=WO-22 result=OK`.

## Journey tier institutionalized (2026-08-29, WO-22 close-out)

- AGENTS.md rule 9: every user-facing feature ships journey units in the
  same change; mutations ship refusal journeys; `scripts/*-journey-*.sh`
  must pass at session close. Doctrine + inventory:
  `lifecycle/JOURNEYS.md` (adding-a-journey recipe included).
- Shared library `tests/lib/journey.sh` (one session plumbing repo-wide;
  recv-by-id + jsonq-complete acceptance — the stdio non-flush lesson
  lives there now); C22a/C22b/C22c + mcp-journey-live.sh refactored
  onto it; C22d gate unit fails on plumbing drift or doctrine decay.
- C22c grew the J6 permutation sweep (12 tool/arg shapes, "well-formed
  answer" invariant). Corpus now 50u; journaled verify per close-out.
