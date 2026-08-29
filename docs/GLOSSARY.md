# GLOSSARY — fleet in plain words

- **fleet** — this product: one binary that runs a deterministic
  software factory on one server, operated by your AI agents.
- **site** — one deployment target: a k3s namespace + a Cloudflare
  account and zone(s). Declared in `ops/SITES.yaml`.
- **k3s** — lightweight Kubernetes; what actually runs your containers
  on the server.
- **component** — a project you are DEVELOPING: code + pipeline + gates.
  Lives in `ops/PROJECTS.yaml`.
- **service** — a thing SERVED on a site: your component once it ships,
  or a self-hosted product. Lives in the site registry
  (`ops/sites/<site>/config/registry.yaml`).
- **stage / promotion** — components move built → dev → stage → prod.
  Each hop is a `promote`, and it re-runs the tests right then.
- **gate** — what a promotion demands: named test units + approvals
  (`lifecycle/gates.yaml`). Stale green counts for nothing.
- **approval / actor policy** — who may approve a hop. Prod approvals
  are physically refused to anyone outside `allowed_actors`
  (`.fleet.yaml`).
- **canary** — a disposable end-to-end drill: register → build → deploy
  → verify over the public internet → tear down. `CANARY PASS` means
  the whole factory works.
- **corpus** — the hermetic test suite (`bash scripts/test.sh`): no
  network, no cluster, deterministic. Reported as units/assertions
  (e.g. `46u/577a`); failing dependencies SKIP their dependents instead
  of passing vacuously.
- **workorder (WO)** — a markdown file carrying one chunk of work: YAML
  front-matter (status, pieces, verify, integrated) that the binary
  validates.
- **piece** — one item inside a workorder, with its own verify step and
  integrated flag.
- **predicate (P1–P6)** — integrity checks over the files: a dirty tree
  needs an open workorder, verifies must be journaled, the journal must
  be untampered, executed workorders must be fully integrated.
  Reported by `fleet check`.
- **journal** — `lifecycle/journal/events.log`: append-only, one line
  per action. The story of the project; tampering is detected.
- **secrets home** — `~/.fleet/secrets/<site>/`: the only place secret
  values live. Outside every git tree, 0600, never printed by fleet.
- **next engine** — `fleet next`: reads the truths and prints the exact
  next legal action with its fix. An agent that only does what next
  prints cannot do it wrong.
- **gatus** — the health-check engine; checks are auto-generated from
  the registry on every deploy.
- **fleetboard** — the consolidated dashboard: every service on the
  site, its live health, and its links on one page, re-rendered
  automatically.
- **kaniko** — builds container images in-cluster from a Dockerfile,
  pinned to a git SHA.
- **tunnel (cloudflared)** — how services get public HTTPS without
  opening ports; fleet reconciles the DNS records for it.
