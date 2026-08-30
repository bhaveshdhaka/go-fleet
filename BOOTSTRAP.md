# BOOTSTRAP.md — fresh-server guided install

One person, one fresh Ubuntu server (22.04/24.04), one paste. The prompt
below drives opencode to set up: bun + node, openchamber (web UI + iPad
PWA), cloudflared tunnel + DNS for a hostname you choose, and this fleet
repo — verifying every step and reporting at the end. No legacy, no
workarounds: if something fails, the agent stops and shows you the output.

## Before you paste (2 minutes, human)

1. Fresh Ubuntu, logged in as **root** over SSH.
2. Install opencode and connect your AI provider key:

   ```bash
   curl -fsSL https://opencode.ai/install | bash
   opencode          # then run /auth inside it and add your provider key
   ```

3. Have ready, you will be asked for them:
   - your **domain** and the **subdomain** for the OpenChamber UI
     (example: `example.com` + `oc`, giving `oc.example.com`)
   - a **Cloudflare API token** for that domain's account with
     **Cloudflare Tunnel Edit + Zone DNS Edit** on the zones you will
     serve (it will create the tunnel and the DNS records) — it gets
     STORED in the fleet secrets home in Step 5
   - approval of a **UI password** (one will be proposed; you can supply
     your own)

## The prompt — copy everything below the line into opencode

---

You are setting up a brand-new single-person Ubuntu server from scratch.
Run as root — this box has exactly one identity, so no user creation, no
permission choreography. Work in `/root`. Execute the mission below
in order. Rules that are absolute:

- Before any step that needs a decision or a credential, ASK me. Do not
  guess hostnames, domains, or tokens.
- The Cloudflare API token is handled with care: keep it in a shell
  variable while making API calls, never echo it, never put it in a
  git-tracked file or log. At the END of Step 5 you STORE it in the
  fleet secrets home (`~/.fleet/secrets/<site>/cloudflare.env`, mode
  0600) — fleet's own ops engine reads it from there for DNS and
  monitoring reconciliation. A fresh install that skips this storage
  leaves `fleet ops dns/monitor/doctor` broken; storing it is required.
- After every step, VERIFY and show me one line of evidence (command +
  result) before moving on.
- If any step fails in a way this brief does not cover: STOP and show me
  the exact output. Do not improvise workarounds and do not skip
  verification.

### Step 0 — system base
`apt update`; install `curl git ca-certificates build-essential jq dpkg bzip2`.
Confirm Ubuntu 22.04/24.04 and x86_64/arm64 (report which).

### Step 1 — bun + node 22
- Install bun: `curl -fsSL https://bun.sh/install | bash` (installs to
  `/root/.bun`; export PATH for this session and add it to `/root/.bashrc`
  if not already present).
- Install node 22 runtime: NodeSource
  (`curl -fsSL https://deb.nodesource.com/setup_22.x | bash -` then
  `apt install -y nodejs`).
- Verify: `bun --version` and `node --version` (expect bun ≥ 1.4,
  node v22.x).

### Step 2 — opencode present
The human already installed opencode and connected a provider. Verify
`opencode --version` only — do not reinstall, do not touch its config.

### Step 3 — openchamber
- ASK me for a UI password (offer to generate one with
  `openssl rand -base64 18` and show it once).
- Install: `curl -fsSL https://raw.githubusercontent.com/openchamber/openchamber/main/scripts/install.sh | bash`
  (it will detect bun — let it).
- Configure and persist — TWO supervisors exist and only one may win:
  `openchamber --ui-password '<the password>'` starts a standalone
  daemon, `openchamber startup enable` installs a systemd USER unit.
  The CLI flag only affects the daemon in memory; the unit reads
  `~/.config/openchamber/startup.env`. So:
  1. `openchamber startup enable` and `loginctl enable-linger root`.
  2. Make sure the line `OPENCHAMBER_UI_PASSWORD=<the password>` exists
     in `~/.config/openchamber/startup.env` (mode 0600) — append it if
     missing; without it a reboot brings the UI up PASSWORDLESS.
  3. Stop the standalone daemon (`openchamber stop`) and let the user
     unit own the service: `systemctl --user restart openchamber`.
  Two processes fighting over port 3000 = one of them crash-looping.
- Verify: `systemctl --user is-active openchamber` is `active`,
  `openchamber status` shows `password: yes`, and
  `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:3000` returns
  a 2xx/3xx.

### Step 4 — cloudflared binary
`curl -fsSL -o /tmp/cloudflared.deb
https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb`
(match arm64 if this is an ARM box) then `dpkg -i /tmp/cloudflared.deb`.
Verify: `cloudflared --version`.

### Step 5 — tunnel + DNS (interactive; you need my inputs here)
1. ASK me for: the subdomain + domain for the UI (e.g. `oc` +
   `example.com` → hostname `oc.example.com`) and my Cloudflare API
   token. Confirm the full hostname back to me before you touch
   anything.
2. Using the token as `Authorization: Bearer` header (variable only):
   - `GET https://api.cloudflare.com/client/v4/user/tokens/verify` →
     expect `success: true, status: active` before anything else.
   - `GET /zones?name=<domain>` → take the zone id AND the account id
     from `result[0].account.id`. A zone-scoped token often returns an
     EMPTY list from `GET /accounts` — do not treat that as failure;
     the zone object carries the account id.
   - `POST /accounts/<acct>/cfd_tunnel` body
     `{"name":"<hostname>","config_src":"cloudflare"}` → capture
     `result.id` and `result.token`. If `token` is absent from the
     response, `GET /accounts/<acct>/cfd_tunnel/<id>/token`.
   - `PUT /accounts/<acct>/cfd_tunnel/<id>/configurations` body
     `{"config":{"ingress":[{"hostname":"<hostname>","service":"http://localhost:3000"},{"service":"http_status:404"}]}}`
   - CNAME `<hostname>` → `<tunnel-id>.cfargotunnel.com` (proxied): first
     `GET /zones/<zid>/dns_records?type=CNAME&name=<hostname>`; create if
     absent, `PUT` retarget if it points somewhere else, no-op if
     already correct. ALWAYS create the NEW tunnel and retarget — if an
     old tunnel with a different name owns the hostname (or a shared
     tunnel serves OTHER hostnames), never reconfigure that tunnel's
     ingress; overwriting it takes other sites down.
3. Install the service so it survives reboots:
   `cloudflared service install <tunnel-token>` (token goes only into
   the systemd unit this creates — that is expected and fine).
4. STORE the Cloudflare API token for fleet (not the tunnel token) —
   this is required, not optional. Ask me to confirm the site name
   (default `hk-03-dev` if this box IS the fleet lab host), then:
   ```bash
   mkdir -p ~/.fleet/secrets/<site> && chmod 700 ~/.fleet/secrets/<site>
   umask 077; printf 'CF_API_TOKEN=%s\n' "$CF_API_TOKEN" \
     > ~/.fleet/secrets/<site>/cloudflare.env; umask 022
   ```
   Verify WITHOUT printing the token:
   `grep -c 'CF_API_TOKEN=' ~/.fleet/secrets/<site>/cloudflare.env`
   (expect `1`). The file is outside every git tree fleet can see.
5. Verify: `cloudflared tunnel info <id>` or service status shows the
   connection up, and
   `curl -s -o /dev/null -w '%{http_code}' https://<hostname>` returns a
   2xx/3xx (DNS can take a minute — retry a few times before failing).

### Step 6 — fleet (the SDLC factory)
```bash
mkdir -p ~/workspaces && cd ~/workspaces
git clone https://github.com/bhaveshdhaka/go-fleet fleet
cd fleet && ./install.sh --json | grep FLEET_INSTALL_OK
bash scripts/test.sh
./scripts/fleet check
./scripts/fleet doctor && ./scripts/fleet next
```
Success criteria: the install line `FLEET_INSTALL_OK prefix=...` (the
machine contract prints in `--json` mode); `scripts/test.sh` prints
`FLEET SUMMARY units_run=46 pass=... fail=0` — `skip=1` is EXPECTED on
a host with no cluster yet (the C4a kubectl tier skips honestly instead
of false-greening; a pod or k3s host runs it for real); `fleet check`
prints `CHECK SUMMARY ... pass=6 fail=0`. Show me those three lines.
If this box IS the fleet lab host, also seed the backup secrets now —
RESTORE.md §3 (`r2.env`: R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY,
RESTIC_PASSWORD into `~/.fleet/secrets/<site>/r2.env`, mode 0600) —
secrets-first is the restore-day law; then prove the password:
`FLEET_RESTIC_BIN=.toolchain/bin/restic` + `restic snapshots` against
`s3:https://<account>.r2.cloudflarestorage.com/<bucket>/<site>` must
list snapshots with rc=0 (repo path = registry `backup:` bucket + site).
Do not run any mutating fleet command beyond this
(`init`/`onboard`/`promote` come later, when I ask for a project).

### Step 7 — openchamber project + the first agent session
1. Register the fleet repo as an openchamber project so it appears in
   the UI. Mechanics: openchamber keeps projects in the `projects`
   array of `~/.config/openchamber/settings.json`; each entry is
   `{id: "path_<unpadded-base64-of-path>", path: "/home/openchamber/workspaces/fleet",
   label: "fleet", addedAt: <ms>}` (copy the shape of an existing
   entry). Verify with `openchamber projects --json` — the path must
   list. (`openchamber session create --dir <path>` also works and
   registers implicitly.)
2. Seed the FIRST agent session for that project with exactly this
   brief: "Read AGENTS.md and follow it. Run
   `./scripts/fleet next` and do what it says; never improvise around a
   refusal. Report what you find and stop." (Verify the session was
   created; do not run fleet mutations yourself here.)
   Expected outcome — this is SUCCESS, not a failure: the session
   drives `next` through the ship path (promote dev → approve dev →
   promote stage) and then REFUSES at `approve prod` because the actor
   policy (.fleet.yaml) reserves prod for human owners. It reports and
   goes idle. That refusal is the evidence the gate works.
3. Confirm to me: fleet is a project in openchamber, the first session
   exists, and the human never needs to run fleet commands by hand.

### Step 8 — the report (print this when everything above is green)
- What was installed: bun, node, opencode (already present), openchamber,
  cloudflared, fleet — with versions.
- How I reach my agents: (a) SSH + `opencode` (TUI), (b) the UI URL
  `https://<hostname>` (works as an app from iPad: browser → Install).
- Where state lives: openchamber in `~/.config/openchamber/`; opencode
  providers/sessions in `~/.local/share/opencode/`; fleet in
  `~/workspaces/fleet` (its own audit journal inside); fleet SECRETS
  (Cloudflare token, tunnel token, service env files) in
  `~/.fleet/secrets/<site>/` — outside every git tree, never printed.
- How updates work: openchamber — Settings → About buttons or
  `openchamber update`; opencode — offered in the same UI (server
  restarts itself); fleet — `git -C ~/workspaces/fleet pull` then
  re-run `bash scripts/test.sh`.
- How to start a real project when I ask:
  `cd ~/workspaces/fleet && ./scripts/fleet init <name>` /
  `./scripts/fleet onboard <name>` — and that agents must read
  `AGENTS.md` first.
- End with: exactly one question — what do I want to build first?

---

### Step 6b — k3s (restore-day path)
If this box will host the fleet site: install the pinned k3s per
`scripts/blocks/01-k3s.sh` (the script maps the kubectl pin to the k3s
fork tag `v<pin>+k3s1` — k3s releases carry that suffix; a bare tag 404s
on get.k3s.io), write the kubeconfig to `/root/.kube/fleet.kubeconfig`
(0600), and verify `kubectl get nodes` Ready with kubeletVersion
`v<pin>+k3s1` before continuing.

### Step 6c — python deny (deterministic)
Append to the GLOBAL opencode config (~/.config/opencode/opencode.jsonc):
"permission": { "bash": { "python3 *": "deny", "python *": "deny", "pip*": "deny" } }
Verify: an agent running `python3 --version` must be refused.
