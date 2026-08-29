# PROJECTS-GUIDE — projects you develop vs products you self-host

Fleet does two different jobs, and keeping them separate keeps everything
in its right place:

| | Projects you DEVELOP | Products you SELF-HOST |
|---|---|---|
| What | New code you (and your agents) write — modern Go is the house style | Existing third-party apps you just want served and monitored (e.g. nzbdav, aiostreams, usenetstreamer) |
| Registered in | the component registry `ops/PROJECTS.yaml` | the site registry `ops/sites/<site>/config/registry.yaml` |
| Lifecycle | built → dev → stage → prod, every hop gated | register → build (optional) → deploy |
| Fleet touches the code? | yes — fleet IS the development process | no — fleet never refactors an existing product |

One house rule, enforced by the legacy gate (C20d): **zero interpreted
runtimes in anything fleet builds or develops** — no python/ruby/node in
your trees, images, or scripts. Self-hosted upstream images are
inventoried as-is, never rewritten.

## Lane A — start a new project (the ship loop)

What you need: a directory with a `Dockerfile`. Minimal modern Go
example:

```dockerfile
FROM golang:1.27 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/app ./cmd/app

FROM gcr.io/distroless/static
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
```

Register it and let the compass drive:

```bash
./scripts/fleet onboard myapp --kind=service --path=myapp \
  --entrypoint=Dockerfile --description=what-it-is
./scripts/fleet next        # prints the exact next legal action, always
```

Then keep doing what `next` says: `promote myapp dev` → `approve myapp
dev` → `promote myapp stage` → `approve myapp prod` (human-gated) →
`promote myapp prod` → `ops build myapp` → `ops deploy myapp` →
`ops verify myapp --expect 200`. Every hop re-runs its tests right now,
and prod approvals are physically refused to anyone outside the allowed
actors.

## Lane B — self-host an existing product

```bash
./scripts/fleet ops register uptime-kuma --host status.example.com \
  --image louislam/uptime-kuma --probe-path / \
  --storage 1Gi:/app/data
./scripts/fleet ops deploy uptime-kuma
```

- `--image` runs an upstream image as-is; `--repo` + `--dockerfile`
  builds your own instead.
- `--probe-path` is where gatus checks health; the service appears on
  the fleetboard dashboard automatically, alongside everything else on
  the site.
- Secrets: declare the KEY names (`--secret API_KEY`); the VALUES go in
  `~/.fleet/secrets/<site>/<service>.env` — never in the repo, never
  printed.
- Pull it back out cleanly any time: `ops remove <service>
  [--delete-data]`.

## Say this to your agent instead of typing

> Read AGENTS.md and follow it. Run `./scripts/fleet next` and do what
> it says; never improvise around a refusal.

The agent gets the same file truths you would, the same refusals, and
the same journal — you stay in the loop at scope approval (start) and
prod acceptance (end).
