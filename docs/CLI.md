# CLI — commands, machine contracts, exit codes

Binary: `cmd/fleet` behind `./scripts/fleet` (pins FLEET_ROOT, rebuilds
from source when stale). Run `fleet --help` for the full list.

## Read verbs (all accept `--json`)

| Command | Text contract | JSON shape |
|---|---|---|
| `status [component]` | `STATUS component=… kind=… stage=…` + `STATUS SUMMARY components=N` | `{"components":[{component,kind,stage}]}` |
| `doctor` | `DOCTOR OK|FAIL checked_components=N issues=M` (+ `DOCTOR ISSUE :: …`) | `{"ok":bool,"issues":[...]}` |
| `next` | `NEXT action=…` + `NEXT reason=…` (+ `NEXT predicate=`) | `{"action":…,"reason":…[,"predicate":…]}` |
| `check` | `CHECK P<n> PASS|FAIL|SKIP detail=…` + `CHECK SUMMARY total=6 pass=N fail=M` | `{"predicates":[...],"total","pass","fail","skip"}` |
| `site list` | `SITE LIST count=N` + `SITE name=… engine=… access=… lab_root=…` | `{"sites":[{name,engine,access,lab_root}]}` |
| `ops status [--site S]` | table (`=== cluster ===` / pods / services) | `{"site":…,"cluster":bool,"services":[...]}` |
| `ops doctor [--site S]` | check lines + `DOCTOR: N problem(s) found` | array of `{check,ok,detail,status}` |
| `ops dns [--site S]` | `dns: <host>: ok|missing|drift|created|fixed` + drift summary | same shape as doctor |

## MCP server (read-only, WO-22 phase 1)

`fleet mcp` serves the Model Context Protocol over stdio — a thin,
READ-ONLY adapter over the verbs above (same `--json` truth, same exit
codes; the self-execed child gets an explicit env allowlist with
`FLEET_ROOT` pinned, rule 7). No mutating verb is reachable.

- **Tools**: `fleet_status`, `fleet_doctor`, `fleet_next`, `fleet_check`,
  `fleet_sites`, `fleet_wo_list`, `fleet_wo_show`, `ops_status`,
  `ops_doctor`, `ops_dns` (report form only).
- **Resources**: `fleet://lifecycle/journal`, `fleet://registry/projects`,
  `fleet://registry/state`, `fleet://registry/sites`,
  `fleet://lifecycle/gates`.
- Errors surface as `isError` tool results (rc≠0), never crashes; doctor
  `ok:false` and `ops status cluster:false` are DATA (valid JSON), not
  errors. Secret VALUES never appear (asserted by C22b).

## Site verbs (mutations)

- `site new <name> [--domain D] [--access …] [--dry-run]` — scaffold;
  dry-run is byte-stable. Contract: `SITE NEW …`
- `site tunnel create <name> --domain D` — CF tunnel + token + records.
  Contract: `TUNNEL CREATED …`
- `infra deploy [--site S]` — registry/cloudflared/gatus + monitor.
  Contract: `INFRA OK site=… applied=N`
- `site canary [--site S] [--domain D]` — full-loop drill.
  Contract: `CANARY PASS site=… host=…`
- `site init <name> --from <lab_root>` — migration (secrets copied to
  the secrets home). Contract: `SITE INIT …`

## Ops verbs (mutations)

`ops register | build | deploy | rollback | remove | dns --apply |
monitor | verify` — machine lines `BUILT`, `DEPLOYED`, `rolled back`,
`removed`, `MONITOR OK`, `-> HTTP`, `registered`.

## Lifecycle verbs

`onboard`, `approve <c> <dev|prod> [who]`, `promote <c> <stage>
[--dry-run] [--skip-gates]`, `verify [units...]` (journals the result),
`wo list|show|new`, `init [dir]`. Contracts: `ONBOARDED component=…`,
`APPROVED component=… stage=…`, `PROMOTED component=… from=… to=…`,
`VERIFY units=… pass=… fail=… result=OK`.

## Exit codes (documented, asserted by C17c)

- **0** — success
- **1** — failure (checks failed, command failed, verify mismatch)
- **2** — usage error or policy refusal (unknown verb, bad args,
  unauthorized actor)

Errors print `FLEET ERROR :: <message>` on stderr; refusals include the
exact fix command.
