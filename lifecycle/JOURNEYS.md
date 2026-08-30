# JOURNEYS — the use-case tier (permanent)

> Journeys are user stories driven through the REAL product surface, not
> API probes. "Does the endpoint return 200" is a contract test; "an
> operator sits down, asks what's broken, gets routed to the fix, and
> verifies it" is a journey. Journeys are how fleet proves features work
> for the people using them — every corpus run, every time
> (`bash scripts/test.sh` runs the whole tier; AGENTS.md rule 9).

## The rules (AGENTS.md rule 9, asserted by tests/C22d)

1. **No journey, no ship.** Every user-facing feature ships its journey
   unit in the SAME change as the feature. A piece without its journey
   does not count as integrated.
2. **Mutations ship refusal journeys.** A tool/verb that can change the
   world must prove, in a journey, that every illegal path is refused
   with the exact fix command — and that the legal path still needs its
   gate (approval, actor policy).
3. **One session plumbing.** Journeys use `tests/lib/journey.sh`
   (stdio session, id-polling with jsonq-complete acceptance, shared
   close asserts). No per-unit copies — C22d fails on drift.
4. **Permutation sweeps.** Journey units loop over the surface (every
   tool × arg shape; every doctor state; every refusal) and assert the
   invariant, not individual samples: answers are well-formed, honest,
   and never empty.
5. **Tiers.** Tier 0 journeys run hermetic fixtures in the corpus; tier 1
   journeys run against the LIVE estate via `scripts/*-journey-*.sh`
   (read-only) and must pass at session close; tier 2 is a real client
   wired by the owner (conversational acceptance); tier 3 is the
   acceptance strategy for future phases (MCP-BRIEF §6).

## Inventory (keep current — C22d asserts the anchors)

| Unit / driver | Surface | Stories pinned |
|---|---|---|
| `tests/C22a_mcp_stdio` | `fleet mcp` wire | read-only tool set is exact; structured content; honest isError; unknown-resource error; clean EOF |
| `tests/C22b_mcp_secret_guard` | `fleet mcp` secrets | canary secret value never appears in any tool/resource output (negative control on disk) |
| `tests/C22c_mcp_operator_journeys` | `fleet mcp` behavior | J1 triage; J2 incident drill (detect→pinpoint→route→fix→re-verify, no cross-tool drift); J3 context assembly; J4 pipelined client batching; J5 refusal UX; J6 permutation sweep (12 tool/arg shapes) |
| `scripts/mcp-journey-live.sh` | live estate (tier 1) | same journeys against the REAL repo; read-only; PASS at session close |
| `tests/C22e_mcp_mutation_refusals` | `fleet mcp --mutations` | mutation tools invisible by default + protocol-error on unregistered call; actor-policy prod refusal with exact fix; allowed-actor legal path; unknown-component/unknown-service refusals |

## Adding a journey for a new feature

1. Write the user story first ("as the operator, when X breaks, I see
   Y and am told Z"). If you can't phrase it as a story, it's a unit
   test, not a journey — both are welcome; label honestly.
2. `source "$FLEET_ROOT/tests/lib/journey.sh"`, set `J_SCRATCH`/`J_JQ`,
   open a session, drive the story, assert outcomes (not payloads —
   payload shapes belong to the contract units).
3. Include the refusal path when the feature can say no.
4. Add the unit to this inventory and (if it pins a new anchor) extend
   C22d. If it's a live-tier story, add it to the matching
   `scripts/*-journey-*.sh` driver.
5. `bash scripts/test.sh` — the corpus runs it every time from now on.

## Enforcement (teeth, not prose)

- **gates.yaml**: the C22 journey units sit in `requires_units` on
  `dev→stage` and `stage→prod` — every promote RE-RUNS the journeys at
  that moment (hard block; asserted by C22d).
- **P7 predicate** (`fleet check`/`next`): an active workorder whose
  pieces reference no journey unit is flagged with the exact fix; the
  only opt-out is `journeys_exempt: true` with a justification (docs/
  fixture-only changes). Enforcement stays at promote, per house policy;
  P7 is the visible drift report agents parse mid-session.
