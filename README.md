# Fleet — a private software lab that builds, verifies and ships itself

Two manuals live in this file:

- **PART 1 — the plain-English manual** (for everyone)
- **PART 2 — the geek + agent manual** (architecture, contracts, fine print)

---

## PART 1 — The plain-English manual

### What does this thing actually do?

Running one command (`./install.sh`) makes your computer download a small
toolbox of well-known, pre-checked software and put it inside this folder —
nowhere else. Then a second command (`bash scripts/test.sh`) proves that
everything the lab promises is actually true on *your* machine, right now.

That is not marketing: this kit contains about one hundred small automatic
checks, and they all have to pass before anyone is allowed to ship anything.

### Who is it for?

- **Anyone** who wants their own little laboratory for building and running
  tiny web programs without risking their computer. Think LEGO kit.
- **Software agents** — computer programs like me that operate repos for you.
  Every rule an agent needs to safely work here is written in `AGENTS.md`,
  in plain files, so you can audit every decision later.

### How do I use it?

1. Have Linux with bash, curl, git (almost all Linux boxes do).
2. Open a terminal in this folder.
3. Type: `./install.sh`

A few minutes of waiting later you are done. Add `--verify` if you also want
the full self-test run afterwards. Your AI assistant can be told simply:
"read AGENTS.md and follow it."

### Fresh Ubuntu install — opencode + openchamber + fleet, the no-drama path

This is the exact shape of the reference deployment this product was built
and operated on (a Ubuntu box, one user, everything inside `$HOME` and one
workspaces root). The rules at the end are what prevents the classic
failures: root-owned files that fight git, `sudo npm` that castrates every
later install, symlink churn in `/usr/local`, and agents tripping over
ambient credentials. Measured working versions: node 20/22, npm 9,
opencode-ai 1.18.x, openchamber 1.20.x, gh 2.98.x, go 1.27.

**1. One dedicated user, one ownership domain.** The agent user owns its
whole HOME and the workspace root. Nothing in those trees is ever created
or touched by root afterwards.

```bash
sudo adduser --uid 1000 agent          # pick a uid; 1000 is what we run
sudo usermod -aG sudo agent            # sudo ONLY for the system packages below
su - agent
```

**2. Base packages (the only sudo you should need):**

```bash
sudo apt update && sudo apt install -y curl git ca-certificates \
  build-essential fuse binutils        # fuse/AppImage only if you use desktop openchamber
```

**3. Node 22 under $HOME (never apt's node, never sudo npm).** Ubuntu's
node is ancient and `sudo npm -g` is the root of all permission pain.
Use nvm, which lives entirely in `$HOME`:

```bash
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/master/install.sh | bash
export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh"
nvm install 22 && nvm alias default 22
npm config set prefix "$HOME/.npm-global"   # global installs stay in $HOME
```

Add to `~/.profile` (then re-login or `source` it):

```bash
export PATH="$HOME/.npm-global/bin:$HOME/.local/bin:$PATH"
```

**4. OpenCode CLI** (the coding agent engine; npm package `opencode-ai` —
this is how the reference box runs it):

```bash
npm install -g opencode-ai
opencode --version                     # 1.18.x measured
```

**5. OpenChamber** (web/PWA control room around opencode; the official
installer wants Node 22+ and installs the `@openchamber/web` CLI):

```bash
curl -fsSL https://raw.githubusercontent.com/openchamber/openchamber/main/scripts/install.sh | bash
openchamber --version                  # 1.20.x measured
openchamber --ui-password 'be-creative-here'
openchamber startup enable             # survive reboots
openchamber status                     # binds 127.0.0.1:3000 by default
openchamber connect-url --qr           # pair phone/laptop over the LAN
openchamber tunnel start --provider cloudflare --mode quick --qr
                                       # optional: reach it from anywhere
```

OpenChamber keeps all state under `~/.config/openchamber/` (sessions,
projects, settings, credentials). Own it, back it up. Binds to localhost —
expose via its tunnel, or `--lan` only on a trusted network.

**6. GitHub CLI** (needed only to push/pull private repos as the agent):

```bash
mkdir -p ~/.local/bin
curl -fsSL https://github.com/cli/cli/releases/latest/download/gh_2.98.0_linux_amd64.tar.gz \
  | tar -xz -C /tmp && mv /tmp/gh_2.98.0_linux_amd64/bin/gh ~/.local/bin/
gh --version
```

Auth: run `gh auth login` once as the agent user, or (inside openchamber)
keep the token in openchamber's own credential store — never paste tokens
into shell history or repo files.

**7. The workspace root + fleet:**

```bash
mkdir -p ~/workspaces && cd ~/workspaces
git clone https://github.com/bhaveshdhaka/go-fleet fleet
cd fleet && ./install.sh               # pinned toolchain -> ./.toolchain, nothing outside
bash scripts/test.sh                   # hermetic corpus: expect FLEET SUMMARY ... fail=0
./scripts/fleet check                  # predicates P1-P6: expect 6/6 PASS
./scripts/fleet doctor && ./scripts/fleet next
```

Then tell any agent, once: *"read AGENTS.md and follow it."* That file is
the operating law: mutations only through `./scripts/fleet` and the
numbered blocks, read-only verbs for everything else, secrets never in
repo files or journals.

**8. Optional — a real cluster tier (what hk-03-dev runs):** install k3s,
then run the SAME shape as the reference deployment: one non-root pod
(uid 1000) for openchamber with a ServiceAccount + RBAC (never a mounted
root-owned kubeconfig), PVCs for `~/.config/openchamber`, and exactly one
hostPath: the workspaces root. fleet talks to clusters ONLY through
site-declared access (`ops/SITES.yaml`; `access: in-cluster` or
`access: kubeconfig:<path>`) — it hard-refuses ambient credentials by
design (AGENTS.md rule 7, enforced by test C12d).

**The rules that prevent the dramas** (each one is a lesson that cost a
real outage or a real debugging session):

- Never `sudo npm install -g`. Global prefix lives in `$HOME`
  (`~/.npm-global`), binaries on PATH from there. `/usr/local` stays
  untouched — no symlinks, no chown chess.
- Never run agents, git, or npm as root in the workspace. If a root-owned
  file ever appears, fix ownership ONCE (`sudo chown -R agent:agent`)
  and find the process that created it — it will keep doing it.
- One node manager (nvm). Mixing apt-node, nvm, and snap-node is how
  "works in my shell, fails in the service" bugs are born.
- One workspaces root (`~/workspaces`), everything cloned beside each
  other; fleet resolves its own root and never relies on the cwd.
- Binaries in `~/.local/bin` or `~/.npm-global/bin` — both on PATH — so
  systemd units, cron, and agents all see the same tools.
- kubectl (if any) always with an explicit `KUBECONFIG` and a HOME it may
  write to; a HOME-less kubectl litters `.kube/` into whatever directory
  it runs in.

### What happens after install? (the part most kits skip)

This kit doesn't just install tools — it runs a **small factory** for making
and shipping software, entirely managed by plain text files:

| You want to... | You tell the factory... |
|---|---|
| See what exists | `./scripts/fleet status` |
| Check everything is healthy | `./scripts/fleet doctor` |
| Say "I approve step X" | `./scripts/fleet approve <name> <step>` or click it on the built-in web dashboard |
| Move a program closer to "live" | `./scripts/fleet promote <name> <stage>` |
| Prove nothing rotted | `bash scripts/test.sh` |

Every action leaves one line in an append-only diary file, and "what is live
where" lives in another plain file — so the whole story of your project can
be read (and audited) from disk, forever.

### Is it safe?

No admin password asked, no writes outside its folder, re-running is always
safe, and downloads come from official sources at exact versions everyone
agrees on. Programs only move forward through fixed checkpoints — a program
cannot "sneak" onto the live stage without passing its tests AND someone
(leaving) an approval file.

### What do I get?

A Go compiler, kubectl, backup and deploy tooling — plus two example
programs: a command-line tool (`fleetctl`) and a mini web dashboard
(`fleethub`) that shows and approves the factory's own work. On any spare
Linux box or cloud server the same kit can stand up a real mini-cluster;
on an ordinary laptop everything builds, verifies and rehearses offline.

### How do I undo everything?

Delete the `.toolchain` folder inside this project. Done.

---

## PART 2 — The geek + agent manual

### Layout

    toolchain.env             single source of truth: every tool pin
    install.sh                bootstrap; contract: FLEET_INSTALL_OK|FAIL|…
    scripts/lib.sh            assertion helpers ([PASS|FAIL|SKIP] protocol)
    scripts/test.sh           DAG runner: topo-sorts units, skips dead deps
    scripts/blocks/01..04     idempotent blocks (k3s wiring, toolchain,
                              go pipeline, k8s apply); strict --dry-run:
                              byte-identical plan, zero mutation
    scripts/fleet             control-plane CLI — thin shim over the Go core
                              (cmd/fleet, module go-fleet; see AGENTS.md rules)
    ci/promote.sh             gate engine: legal hops, approvals checked,
                              gate test units RE-RUN at promotion time
    ops/PROJECTS.yaml         master registry = deployment INTENT
    ops/state/deployments.yaml runtime TRUTH (written ONLY by ./fleet)
    lifecycle/                stages, gates.yaml, approvals/, journal/
    infra/k8s/*.yaml          declarative manifests (block 04 applies)
    apps/fleetctl apps/fleethub  stdlib-only reference binaries
    tests/C*                  one dir per unit: run.sh (+ DEPS file)
    AGENTS.md                 THE agent protocol (rules + machine contracts)
    workorders/WO-*.md        self-contained execution briefs
    HANDOVER.md               measured end-of-session continuity

### Guarantees enforced by the corpus

- Determinism: dry-runs and repeat builds assert byte-equality
  (`assert_zero_delta`); drift fails loudly.
- No false greens: failing dep SKIP their dependents, never pass vacuously.
- Gate honesty: `promote` re-executes listed units NOW; stale logs worthless.
- Idempotency everywhere: repeat approve/promote/install are zero-mutation.
- Full Tier-0 corpus is network-free and cluster-free (17 units / 176 assertions).

### The SDLC loop in one paragraph

`ops/PROJECTS.yaml` declares intent → `ci/promote.sh` walks each component
through `built → dev → stage → prod`, enforcing per-hop gates (green unit
runs + approval files) → successful hops append to
`lifecycle/journal/events.log` and rewrite `ops/state/deployments.yaml` →
block 04 materializes prod into a real cluster during VM-tier drills.
Everything is inspectable with `cat`. Agents must obey `AGENTS.md`: mutations
only via `./fleet` / numbered blocks, read-only via status/doctor/--dry-run.

### fleethub endpoints

    GET  /healthz        -> ok
    GET  /               -> HTML dashboard (components × stages × approvals)
    GET  /api/projects   -> JSON mirroring registry+state+approval truth
    POST /approve        -> writes the SAME approval files as ./fleet approve
                            (idempotent; journal line format identical)

Bind defaults to 127.0.0.1:8099 (`FLEETHUB_ADDR` overrides).

### Extending

New capability = one block honoring the dry-run contract + one `tests/<UNIT>/`
directory (`run.sh` sources lib.sh, ends with `finalize`; optional DEPS).
Discovery and topo-ordering are automatic. New component = one registry entry
+ pipeline file + gates entries; then `./fleet doctor` must go ALL CLEAR.

### Tiers

Tier 0 (this repo): hermetic container-safe verification, offline.
Tier 1 (next): `scripts/test-onvm.sh` on a disposable Ubuntu host — real k3s
daemon, live apply, rollback drill.

### Uninstall / reset

    rm -rf .toolchain      # every artifact lives there; nothing else persists
