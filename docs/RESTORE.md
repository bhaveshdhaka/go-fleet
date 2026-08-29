# RESTORE — format-day runbook (site hk-03-dev)

A wiped box back to the full estate. The order is deliberate: secrets
FIRST (a dead run must never leave secrets unrecoverable — journaled
lesson, WO-20), then infra, then deploys, then data. Every step prints a
machine contract; if a contract line is missing, STOP and show the
output. Agent-driven: paste the step's command and let the session run
it — never improvise around a refusal.

## 0. What must exist OUTSIDE the box (password manager)

- **Cloudflare API token** — Cloudflare Tunnel Edit + Zone DNS Edit.
- **R2**: `R2_ACCESS_KEY_ID` + `R2_SECRET_ACCESS_KEY` + `RESTIC_PASSWORD`
  (bucket `hk-03-backup`; endpoint + repo path are recorded in the site
  registry's `backup:` section, restored with the repo clone).
- **GitHub PAT** (repo scope) — go-fleet and 1ed-ge are private.
- Note: openchamber/opencode session state is NOT backed up (owner
  decision: rebuildable).

## 1. Bootstrap the box

Paste BOOTSTRAP.md into opencode; answer: domain `bhavesh.hk` +
subdomain `oc` (→ oc.bhavesh.hk), the CF token, a UI password, site
name `hk-03-dev`. Verify before continuing:

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://oc.bhavesh.hk   # 2xx/3xx
cd /root/workspaces/fleet && bash scripts/test.sh | tail -1      # fail=0
./scripts/fleet check | tail -1                                  # pass=6 fail=0
```

## 2. The cluster

```bash
cd /root/workspaces/fleet
bash scripts/blocks/01-k3s.sh /root/.fleet/k3s-state    # pinned k3s, waits for Ready
cat /etc/rancher/k3s/k3s.yaml > /root/.kube/fleet.kubeconfig && chmod 600 /root/.kube/fleet.kubeconfig
```

## 3. Seed r2.env — the only hand-typed secrets step

`ops restore-secrets` decrypts the R2 repo, so the R2 creds must exist
before anything else can come back. Three keys; the repository path is
composed automatically from the registry's `backup:` section.

```bash
mkdir -p /root/.fleet/secrets/hk-03-dev && chmod 700 /root/.fleet/secrets/hk-03-dev
umask 077; cat > /root/.fleet/secrets/hk-03-dev/r2.env <<'EOF'
R2_ACCESS_KEY_ID=<from manager>
R2_SECRET_ACCESS_KEY=<from manager>
RESTIC_PASSWORD=<from manager>
EOF
umask 022
```

## 4. Restore the secrets home (FIRST — everything else keys off it)

```bash
export FLEET_RESTIC_BIN=/root/workspaces/fleet/.toolchain/bin/restic
cd /root/workspaces/fleet
./scripts/fleet ops restore-secrets --site hk-03-dev
# expect: SECRETS RESTORED site=hk-03-dev snapshot=latest files=9
```

Re-running deliberately overwrites only with `--force` (r2.env keeps
your live keys otherwise). The restored files include the old
`tunnel.env`, `github.env`, `dashboard.env` and every service env.

## 5. Doctor

```bash
./scripts/fleet ops doctor --site hk-03-dev
```

Read-only; every problem prints its exact fix. Iterate until ALL CLEAR.

## 6. Infra (registry, cloudflared, gatus, fleetboard)

```bash
./scripts/fleet infra deploy --site hk-03-dev
# expect: INFRA OK site=hk-03-dev applied=N
```

cloudflared comes back with the restored tunnel token — the tunnel
object survived in Cloudflare, so every existing CNAME (arjun.hk,
mock.1ed.ge, the trio hosts) works again with NO DNS changes.

## 7. Deploy the trio (upstream images; secrets resolve from the restored home)

```bash
./scripts/fleet ops deploy --site hk-03-dev aiostreams nzbdav usenetstreamer
# expect: DEPLOYED <svc> per service
```

## 8. Restore their data

```bash
./scripts/fleet ops restore --site hk-03-dev aiostreams nzbdav usenetstreamer --plan
# byte-stable preview: RESTORE PLAN <svc> snapshot=latest: scale 0 -> restore Job -> scale back
./scripts/fleet ops restore --site hk-03-dev aiostreams nzbdav usenetstreamer --snapshot latest
# expect: RESTORED <svc> snapshot=latest (quiesce -> restore Job, waits up to 900s each)
```

## 9. Verify

```bash
./scripts/fleet ops verify aiostreams --expect 200
./scripts/fleet ops verify nzbdav --expect 200
./scripts/fleet ops verify usenetstreamer --expect 200
```

Then the fleetboard dashboard (slug from the restored
`dashboard.env`): every service, live gatus health.

## 10. Rebuild-from-source components

These are code, not data — rebuilt, not restored:

- **arjun-hk**: source in the fleet repo (`apps/arjun-hk`) —
  `ops build --site hk-03-dev arjun-hk` → `ops deploy --site hk-03-dev arjun-hk`.
- **1edge / 1edge-mocks**: source in the private 1ed-ge repo (clone with
  the PAT to `/root/workspaces/1ed-ge`) — same build → deploy path
  (mocks.Dockerfile is `rewrite/mocks.Dockerfile`).
- Their data snapshots exist too (nightly 04:00) — restore them with
  step 8's verb if wanted.

## Critical notes

- `openchamber` and `docker-registry` are **registry-critical**: fleet
  refuses to quiesce them — they come back via bootstrap/redeploy,
  never a data restore.
- Retention is 7d/4w — the R2 repo must keep receiving nightly backups
  once the estate is up (verify with `ops backup --all` cadence or the
  04:00 CronJobs).
- After the estate is green: journal the restore
  (`FLEET_WO=WO-21 ./scripts/fleet verify`) and commit + push.
