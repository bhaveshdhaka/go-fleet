# QUICKSTART — fresh Ubuntu box to live public site

Prerequisites: Ubuntu 22.04/24.04, root or a dedicated user, a domain on
Cloudflare, and a **Cloudflare API token** with **Cloudflare Tunnel Edit +
Zone DNS Edit** for your zones (create one at
<https://dash.cloudflare.com/profile/api-tokens> — Cloudflare's own wizard
covers the rest). The guided, agent-driven version of this page is
[BOOTSTRAP.md](../BOOTSTRAP.md): paste it into opencode and never type
these commands by hand.

## 0. Install fleet

```bash
sudo apt update && sudo apt install -y curl git build-essential
mkdir -p ~/workspaces && cd ~/workspaces
git clone https://github.com/bhaveshdhaka/go-fleet fleet && cd fleet
./install.sh                 # pinned toolchain -> ./.toolchain
bash scripts/test.sh         # expect: FLEET SUMMARY ... fail=0
./scripts/fleet check        # expect: CHECK SUMMARY total=6 pass=6
```

## 1. Stand up the cluster (or point at one)

Single-node k3s — lightweight Kubernetes, the app runtime on your
server (root):

```bash
curl -sfL https://get.k3s.io | sh -          # scripts/blocks/01-k3s.sh has the pinned variant
sudo cat /etc/rancher/k3s/k3s.yaml > ~/.kube/fleet.kubeconfig && chmod 600 ~/.kube/fleet.kubeconfig
```

## 2. Scaffold the site (offline, byte-stable dry-run first)

```bash
./scripts/fleet site new mysite --domain example.com \
  --access kubeconfig:$HOME/.kube/fleet.kubeconfig
./scripts/fleet site new other --dry-run   # prints the plan, writes nothing
```

This creates `ops/sites/mysite/` (registry skeleton with deliberate
`TODO_*` markers, state, templates), a SITES.yaml entry, and the secrets
home `~/.fleet/secrets/mysite/`.

## 3. Store the Cloudflare API token (required)

```bash
mkdir -p ~/.fleet/secrets/mysite && chmod 700 ~/.fleet/secrets/mysite
umask 077; printf 'CF_API_TOKEN=%s\n' 'YOUR_TOKEN' \
  > ~/.fleet/secrets/mysite/cloudflare.env; umask 022
```

Values live ONLY under `~/.fleet/secrets/<site>/` — outside every git
tree, 0600, never printed by any fleet command.

## 4. Create the tunnel + record the zone

```bash
./scripts/fleet site tunnel create mysite --domain example.com
```

Fleet calls the Cloudflare API: creates the named tunnel
(`config_src: cloudflare`), stores the tunnel token in
`~/.fleet/secrets/mysite/tunnel.env`, and records tunnel id/name/zone in
the site registry. Fill the remaining `TODO_ACCOUNT_ID` in
`ops/sites/mysite/config/registry.yaml`.

## 5. Deploy the infrastructure

```bash
./scripts/fleet infra deploy --site mysite
```

Deploys the in-cluster docker registry, cloudflared (token from
`tunnel.env`), gatus, and the fleetboard dashboard; then syncs
monitoring. Contract: `INFRA OK site=mysite applied=3`.

Your **fleetboard** — the consolidated health/status page for everything
registered on the site — is served at
`https://example.com/<DASHBOARD_SLUG>/` (the slug is the
`DASHBOARD_SLUG=` value in `~/.fleet/secrets/mysite/dashboard.env`).
Every service you register or deploy from here on shows up on it with
its live gatus health.

## 6. Prove it end to end

```bash
./scripts/fleet site canary --site mysite --domain example.com
```

Registers a canary, builds it with kaniko, deploys it, verifies
`https://canary.example.com` over the public internet, then tears it
down — `CANARY PASS`. Your factory is proven. Now ship something real:

```bash
./scripts/fleet onboard myapp && ./scripts/fleet next   # follow the loop
```

## Troubleshooting

Run `./scripts/fleet ops doctor --site mysite` (read-only) first — every
problem prints its exact fix, and every refusal names the key or file it
wants. Secrets problems are always "missing file/key", never values.
