# CONCEPTS — the file contracts fleet enforces

Fleet is a set of **file contracts** plus one binary that enforces them.
Everything is plain text; everything is auditable with `cat`.

## The two truths

- **Deployment INTENT** — `ops/PROJECTS.yaml` (components) and
  `ops/sites/<site>/config/registry.yaml` (site services: hosts, ports,
  storage, resources, env, secrets *names*). What SHOULD exist.
- **Runtime TRUTH** — `ops/state/deployments.yaml` (component stages)
  and `ops/sites/<site>/state/{deployed,builds}.json` (what is actually
  built/deployed). Written ONLY by the binary. Hand-editing state is the
  one unforgivable sin; predicates and the journal catch it.

## Sites

A site is one deployment target: a k3s namespace + a Cloudflare account
+ one or more DNS zones. Declared in `ops/SITES.yaml` with EXPLICIT
access (`in-cluster` or `kubeconfig:<path>`) — fleet never uses ambient
credentials. Secrets live outside the repo: `$FLEET_SECRETS_HOME/<site>/`
or `$HOME/.fleet/secrets/<site>/` (`cloudflare.env`, `tunnel.env`,
`<service>.env`). Values are never printed; key names are.

## Components, stages, gates

A component (in PROJECTS.yaml) moves built→dev→stage→prod via
`fleet promote`. Each hop re-runs its listed test units RIGHT NOW
(`lifecycle/gates.yaml`) — stale green counts for nothing — and checks
approval files. `.fleet.yaml` sets the **actor policy**: prod approvals
are refused for actors outside `allowed_actors` (e.g. only
`owner`/`owner-via-agent`); the refusal prints the exact fix.

## The journal

`lifecycle/journal/events.log` — append-only, one line per action
(plus `#` comment lines for verify results and incidents). P6 detects
tampering. The journal is the story of your project.

## Workorders

`workorders/WO-*.md` carry YAML front-matter (schema v1) the binary
validates: status, plan link, pieces with verify + integrated flags.
Predicates P1–P6 (reported by `fleet check`) keep authoring honest:
no dirty-tree without an open workorder, no unintegrated EXECUTED
pieces, no unjournaled verifies, no tampered journal.

## The ops engine

Site-level operations:
`ops register` (rich runtime surface: storage, mounts, resources,
probePath, runAsUser, serviceAccount, args), `ops build` (kaniko, git-SHA
pinned, date tags), `ops deploy` (secret → deployment → service → PVC →
DNS → tunnel → monitoring → state), `ops rollback`, `ops remove
[--unregister --delete-data]`, `ops dns --apply` (CNAME + tunnel ingress
reconcile across zones), `ops monitor` (gatus + fleetboard), `ops verify`
(public HTTP smoke). Mutating verbs honor `--dry-run` byte-equality.

## The next engine

`fleet next` reads the truths and prints the exact next legal action —
lifecycle hops first, then the ops ladder (build → deploy) for prod-stage
components. Same state → same output. It is the agent loop's compass and
is proven by a golden replay test.

## Testing

`scripts/test.sh` runs a dependency-ordered DAG of hermetic units
(`tests/C*`) — no network, no cluster. Failing dependencies SKIP their
dependents (never vacuous green). Dry-run byte-equality is asserted
everywhere a planner exists.
