# Fleet — the agent-operated release factory for one server

Fleet turns **one Ubuntu box + one k3s node + one Cloudflare account**
into a software factory that builds, verifies, ships and watches your
projects — and is operated by your AI agents, not by you clicking.

One static Go binary. Zero dependencies. Every action journaled in plain
files. Every behavior proven by a hermetic test corpus (46 units /
577 assertions) that runs on your machine, offline, before anything ships.

```
./install.sh                 # pinned toolchain, hermetic
bash scripts/test.sh         # 46u/577a — prove the install
./scripts/fleet next         # the exact next legal action, always
```

---

## Who it's for

One developer (plus their AI agents) shipping a handful of small
services on a single server they own. If you run Kubernetes clusters for
a living you want Argo/Flux; if you want a pretty UI for docker-compose
you want Coolify. Fleet is the third thing: a **deterministic control
plane for a one-person, agent-operated fleet**.

## What you get (and the problem each piece solves)

| You get | The problem it solves |
|---|---|
| **`fleet next`** — a guidance engine over your files | Agents never guess: same state → same next command, refusals print their exact fix. Model quality changes speed, never correctness. |
| **Gated promotion** built→dev→stage→prod | Nothing reaches production without passing its tests *right now* (gates re-run at promote time; stale green is worthless) and a human approval on prod (agent refusals are enforced by policy, not by hope). |
| **Real deploys to real k3s** (`ops build/deploy/rollback/remove`) | kaniko builds pinned to git SHA + date tags, in-cluster registry with cache, rollouts, rollback, teardown with PVC lifecycle. The boring parts, done identically every time. |
| **Public HTTPS in minutes** (Cloudflare Tunnel + DNS) | `ops dns --apply` reconciles CNAMEs and tunnel ingress from your registry across multiple zones — drift fails loudly, `--apply` heals. |
| **Monitoring auto-generated from the registry** | gatus + the **fleetboard** dashboard re-render on every deploy; every service's health page exists because the registry says so. |
| **Secrets that never touch git** | One mechanism: `~/.fleet/secrets/<site>/`, outside every repo, 0600, name-checked but never printed. |
| **Byte-equality dry-runs** | `--dry-run` output is byte-identical across runs — you see exactly what will change before anything changes. |
| **An append-only journal** | The whole story of your project — every build, promote, deploy, verify — readable with `cat`, forever. |
| **`--json` on every read verb** | Scripting agents get the same truth as humans; exit codes (0/1/2) never surprise. |
| **A fresh-install path** | `site new` → `site tunnel create` → `infra deploy` → `site canary`: bare repo to live public site in four verbs, with a canary proving the whole loop end to end. |

## Your first project (the loop your agent runs)

```
./scripts/fleet onboard myapp        # registry + pipeline + state
./scripts/fleet next                 # → promote myapp dev
./scripts/fleet promote myapp dev    # gates re-run NOW
./scripts/fleet approve myapp dev    # middle hop: agent auto-approvable
./scripts/fleet promote myapp stage
./scripts/fleet approve myapp prod   # prod: human-gated by the actor policy
./scripts/fleet promote myapp prod
./scripts/fleet ops build myapp      # kaniko, sha-pinned
./scripts/fleet ops deploy myapp     # rollout + DNS + tunnel + monitoring
./scripts/fleet ops verify myapp --expect 200
```

`fleet next` drives every step — an agent that only does *what next
prints* cannot do it wrong. The walk is enforced by a golden replay test
(`tests/C17a_ship_path_replay`), not by documentation.

Bringing a project in — a new one you are developing, or an existing app
you just want served and monitored — see
[docs/PROJECTS-GUIDE.md](docs/PROJECTS-GUIDE.md) for the two lanes and
[docs/GLOSSARY.md](docs/GLOSSARY.md) for the vocabulary.

## Fresh server install

The no-drama path is a single paste into opencode: **[BOOTSTRAP.md](BOOTSTRAP.md)**
installs opencode + openchamber, registers fleet as an openchamber
project, seeds the first agent session, stands up the k3s + tunnel +
DNS, and **stores your Cloudflare token in the fleet secrets home**
(`~/.fleet/secrets/<site>/cloudflare.env` — required; the ops engine
reads it from there). Then: `site new` → `site tunnel create` →
`infra deploy` → `site canary`. See [docs/QUICKSTART.md](docs/QUICKSTART.md).

## Agent mode

Tell your agent two sentences:

> Read AGENTS.md and follow it. Run `./scripts/fleet next` and do what
> it says; never improvise around a refusal.

Fleet was built agentic-first: machine-parseable summary lines, `--json`,
self-correcting fix commands, workorder front-matter the binary
validates, an approval-actor policy that physically refuses unauthorized
prod approvals, and a journal that catches tampering. Humans stay in the
loop at **scope approval** (start) and **prod acceptance** (end); agents
run everything between.

## Architecture in one paragraph

Deployment INTENT is `ops/PROJECTS.yaml` + site registries; runtime TRUTH
is `ops/state/` + site state files, written only by the binary. The Go
core (`cmd/fleet`, stdlib-only, pinned toolchain, byte-identical hermetic
builds) enforces file contracts: workorder schema, gates, actor policy,
journal integrity. k3s access is always site-declared (in-cluster SA or
explicit kubeconfig — never ambient credentials). Tests are a DAG of
hermetic units (`scripts/test.sh`) that skip dead dependencies instead of
passing vacuously. Read `AGENTS.md` (the law) and
[docs/CONCEPTS.md](docs/CONCEPTS.md).

## How it compares

| | Fleet | Kamal | Coolify/Dokploy | Flux/Argo CD | Skaffold/Tilt |
|---|---|---|---|---|---|
| Target | 1 dev + agents, 1 node | small teams, plain servers | UI-first self-hosted PaaS | teams, real clusters | dev loops |
| Control plane | none (static binary) | none | server + UI | in-cluster controllers | local |
| Deterministic plan/apply with byte-equality | ✅ | ❌ | ❌ | partial | ❌ |
| Hermetic test corpus proving itself | ✅ 46u/577a | ❌ | ❌ | ❌ | ❌ |
| Human gates + actor policy + journal | ✅ | ❌ | ❌ | partial | ❌ |
| Agent-native (next engine, --json, machine contracts) | ✅ | ❌ | ❌ | ❌ | ❌ |
| Cloudflare tunnel/DNS reconciliation | ✅ | ❌ | partial | ❌ | ❌ |

## Honest limits

One node, one operator, one Cloudflare account. No multi-tenant RBAC (by
design), no GitOps controllers, no Helm — plain files and one binary.
If you outgrow this, the file contracts migrate upward cleanly.

## Uninstall

Delete the repo and `rm -rf ~/.fleet`. The cluster keeps running your
services (they're plain k8s objects); `ops remove --delete-data` tears a
service down properly first if you want that.

MIT licensed. Built by agents, gated by humans.
