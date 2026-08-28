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
`apt update`; install `curl git ca-certificates build-essential jq dpkg`.
Confirm Ubuntu 22.04/24.04 and x86_64/arm64 (report which).

### Step 1 — bun + node 22
- Install bun: `curl -fsSL https://bun.sh/install | bash` (installs to
  `/root/.bun`; export PATH for this session and add it to `/root/.bashrc`
  if not already present).
- Install node 22 runtime: NodeSource
  (`curl -fsSL https://deb.nodesource.com/setup_22.x | bash -` then
  `apt install -y nodejs`).
- Verify: `bun --version` and `node --version` (expect bun 1.3.x,
  node v22.x).

### Step 2 — opencode present
The human already installed opencode and connected a provider. Verify
`opencode --version` only — do not reinstall, do not touch its config.

### Step 3 — openchamber
- ASK me for a UI password (offer to generate one with
  `openssl rand -base64 18` and show it once).
- Install: `curl -fsSL https://raw.githubusercontent.com/openchamber/openchamber/main/scripts/install.sh | bash`
  (it will detect bun — let it).
- Configure and persist:
  `openchamber --ui-password '<the password>'`,
  `openchamber startup enable`, `loginctl enable-linger root`.
- Verify: `openchamber status` shows the server on 127.0.0.1:3000, and
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
   - `GET https://api.cloudflare.com/client/v4/accounts` → take the
     account id.
   - `GET /zones?name=<domain>` → take the zone id.
   - `POST /accounts/<acct>/cfd_tunnel` body
     `{"name":"<hostname>","config_src":"cloudflare"}` → capture
     `result.id` and `result.token`. If `token` is absent from the
     response, `GET /accounts/<acct>/cfd_tunnel/<id>/token`.
   - `PUT /accounts/<acct>/cfd_tunnel/<id>/configurations` body
     `{"config":{"ingress":[{"hostname":"<hostname>","service":"http://localhost:3000"},{"service":"http_status:404"}]}}`
   - CNAME `<hostname>` → `<tunnel-id>.cfargotunnel.com` (proxied): first
     `GET /zones/<zid>/dns_records?type=CNAME&name=<hostname>`; create if
     absent, `PUT` retarget if it points somewhere else, no-op if
     already correct.
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
cd fleet && ./install.sh
bash scripts/test.sh
./scripts/fleet check
./scripts/fleet doctor && ./scripts/fleet next
```
Success criteria: `./install.sh` prints a line containing
`FLEET_INSTALL_OK`; `scripts/test.sh` prints `FLEET SUMMARY ... fail=0`;
`fleet check` prints `CHECK SUMMARY ... pass=6 fail=0`. Show me those
three lines. Do not run any mutating fleet command beyond this
(`init`/`onboard`/`promote` come later, when I ask for a project).

### Step 7 — the report (print this when everything above is green)
- What was installed: bun, node, opencode (already present), openchamber,
  cloudflared, fleet — with versions.
- How I reach my agents: (a) SSH + `opencode` (TUI), (b) the UI URL
  `https://<hostname>` (works as an app from iPad: browser → Install).
- Where state lives: openchamber in `~/.config/openchamber/`; opencode
  providers/sessions in `~/.local/share/opencode/`; fleet in
  `~/workspaces/fleet` (its own audit journal inside); fleet SECRETS
  (Cloudflare token, service env files) in `~/.fleet/secrets/<site>/` —
  outside every git tree, never printed.
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
