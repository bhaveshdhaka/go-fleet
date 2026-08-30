# MCP BRIEF — what it is, where it stands, and what it means for fleet

> Research-grounded as of 2026-08-29. Decision requested: green-light
> WO-22 (`fleet mcp`). **UPDATE 2026-08-29: phase 1 BUILT and corpus-
> green** — `fleet mcp` stdio serves 10 read-only tools + 5 contract
> resources (official Go SDK v1.7.0, vendored; C22a contract unit, C22b
> secret-leak guard). Phases 2–3 below remain parked until re-opened.

## 1. What MCP is

An open protocol (JSON-RPC over stdio or HTTP) that lets any AI client —
Claude, ChatGPT, Cursor, Copilot, Gemini — discover and call **tools**,
read **resources**, and use **prompts** exposed by any server. One
integration reaches every client. USB-C for agent-to-tool wiring.

## 2. Where it stands (2026-08)

- **Standards war over.** Anthropic launch Nov 2024; OpenAI adopted
  Mar 2025, Microsoft Jul 2025, AWS Nov 2025; Dec 9 2025 Anthropic
  donated MCP to the Linux Foundation's Agentic AI Foundation
  (co-founders Anthropic/Block/OpenAI; platinum AWS/Google/Microsoft/
  Cloudflare/Bloomberg). No single vendor owns it.
- **Scale:** ~97M monthly SDK downloads; 21k-23k public servers; 41% of
  enterprise software orgs in production; every major AI vendor ships
  native support.
- **Spec 2026-07-28 (biggest revision yet):** stateless core (no
  handshake, no session pinning — plain HTTP load balancing works),
  `server/discover`, **Tasks extension** (long-running ops return a
  taskId — built for things like kaniko builds), cacheable tool lists,
  OAuth 2.1 + PKCE mandatory for remote servers.
- **The gap = the opportunity:** only 8.5% of servers implement the
  mandatory OAuth 2.1; 18% have any access scoping; 53% hard-code
  credentials. Governance is the ecosystem's weakest point — exactly
  what fleet already enforces in binary.

## 3. Why fleet x MCP fits cleanly

| Fleet today | MCP surface |
|---|---|
| `fleet next` guidance engine | tool `next` — same state -> same action |
| read verbs with `--json` | tools with structured output |
| actor policy + prod human gate | human-in-the-loop pattern (Pinterest's published enterprise reference; fleet enforces it in the BINARY) |
| append-only journal, site registries | MCP resources |
| long kaniko builds/deploys | Tasks extension |
| single mutation authority | MCP server = thin adapter; the binary stays the only writer |

Implementation: the **official Go SDK**
(`github.com/modelcontextprotocol/go-sdk`, maintained with Google,
Tier-1, 2026-07-28 betas available). Fleet is a stdlib-only static Go
binary — a `fleet mcp` subcommand embeds the server natively:
`mcp.Server` + `mcp.AddTool` wrapping existing verbs,
`StreamableHTTPHandler` for remote, stdio for local. No new daemons,
no new languages.

**Product shape:** today fleet is agent-operated via opencode/openchamber
only. With `fleet mcp` behind the customer's Cloudflare tunnel (Cloudflare
Access covers the OAuth 2.1 requirement), the customer's fleet becomes
drivable from ANY MCP client while the prod gate stays binary-enforced.
Then list in the official MCP Registry. Pitch: *"the agent-operated
release factory any AI client can drive, with human gates that hold."*


## 4. Honest risks

- Spec velocity: July revision deprecated Roots/Sampling — pin the SDK.
- Go SDK is in beta for the new spec — expect churn.
- The CLI verbs stay the single mutation authority; MCP must never drift
  from that (adapter, not second brain).

## 5. Recommended sequence (WO-22, through the repo's own process)

1. ~~`fleet mcp` stdio — read verbs only~~ **DONE 2026-08-29** (10 tools,
   5 resources, SDK vendored, C22a + C22b corpus units)
2. mutation tools gated by the existing actor policy (promote/deploy),
   Tasks extension for build/deploy — **PARKED**
3. Streamable HTTP + Cloudflare Access behind the customer tunnel,
   official registry listing, docs — **PARKED**

## 6. How this is tested (tiered, like everything else in fleet)

| Tier | What proves it | Status |
|---|---|---|
| 0 — wire contract | C22a: handshake, exact read-only tool set, structured content, honest isError mapping, clean EOF. C22b: canary secret value never appears in any output (negative control on disk). | corpus-green |
| 0 — journeys | C22c: operator user stories, not protocol probes — morning triage; **incident drill** (break state → agent DETECTS via doctor, PINPOINTS via issue text, gets ROUTED by next, fix, re-verify, cross-tool no-drift); context assembly for a handoff (wo list/show + journal resource); **pipelined client batching** (three calls fired without waiting); refusal UX (actionable error text). | corpus-green |
| 1 — live estate | `scripts/mcp-journey-live.sh`: the same journeys against the REAL repo over the real stdio surface — status/doctor/next/wo/journal/ops_status on the live site. Read-only by construction. | run at close, PASS |
| 2 — real client | Wire an actual MCP client (below) and run the same journeys conversationally; the owner's daily-driver use is the acceptance test. | owner wiring |
| 3 — phase 2/3 acceptance (when built) | gate-refusal journeys (agent ATTEMPTS `promote` without approval → refused with fix command, journaled), actor-policy impersonation (FLEET_ACTOR forging must not pass prod gates), Tasks long-build journeys with progress + cancel, staging tenant behind Cloudflare Access with a real remote client BEFORE registry listing. | parked with phases 2–3 |

Phase-2 mutations MUST ship with their journey unit in the same change —
a mutation tool without a refusal journey does not pass `check`.

## 7. Wiring a real client (phase 1, stdio)

opencode (`~/.config/opencode/opencode.json` or project `opencode.json`):

```json
{ "mcp": { "fleet": { "type": "local",
    "command": ["<absolute-path>/dist/fleet", "mcp"],
    "enabled": true } } }
```

Claude Code:

```bash
claude mcp add fleet -- <absolute-path>/dist/fleet mcp
```

Any generic client: command `<absolute-path>/dist/fleet`, args `["mcp"]`,
transport stdio. The binary pins FLEET_ROOT itself (or honors the
client-provided `FLEET_ROOT` env); no ambient credentials are consulted.
