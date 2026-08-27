---
wo: WO-7
title: Ops engine (read-only parity with sos-lab)
status: EXECUTED
plan: PLAN.md
pieces:
  - id: 1
    title: site model (ops/SITES.yaml) + fleet site list + C12a
    verify: go vet + go test ./internal/... + bash scripts/test.sh
    integrated: true
  - id: 2
    title: sos-lab contract readers (registry v2, state JSON, secret names)
    verify: go vet + go test ./internal/... + bash scripts/test.sh
    integrated: true
  - id: 3
    title: fleet ops status parity (nodes/pods verbatim, services table)
    verify: live parity diff vs ./lab status + corpus
    integrated: true
  - id: 4
    title: fleet ops doctor parity (all checks incl cloudflare, --json) + C12b-C12d
    verify: live parity diff vs ./lab doctor + corpus
    integrated: true
---

# WO-7 — Ops engine: site model + read-only parity against live hk-03-dev

> **Status:** IN PROGRESS this session · Owner directive: goal message
> 2026-08-27 (WO-5..WO-10 in order; zero mutations in WO-7). Owner also
> directed (2026-08-27): "just do locally sync later" — WO-6 gh deferred.

## Plan section (decisions, dated 2026-08-27)

- Site model: `ops/SITES.yaml` declares managed sites. hk-03-dev entry:
  engine sos-lab, lab_root `../sos-lab`, namespace sos-lab, and — the
  rule-7 centerpiece — `access:` is EXPLICIT per site (`in-cluster` here;
  `kubeconfig:<path>` supported). fleet NEVER resolves a cluster by ambient
  fallback: the ops runner constructs a temp kubeconfig (tokenFile/ca
  pointers into the pod serviceaccount, mode 0600, deleted after) and execs
  kubectl with ONLY that KUBECONFIG in its env — C12d asserts the kubectl
  env never inherits the ambient environment.
- Parity contract (measured from labctl source + live runs, replicated
  byte-for-byte in Go):
  - status: `=== cluster ===` + `kubectl get nodes -o wide` verbatim;
    `=== pods (sos-lab) ===` + pods -o wide verbatim; `=== services
    (registry) ===` table sorted by name, 2-space ljust columns
    service/state/port/deployed-tag/sha/url; sha = deployed.git_sha or
    builds.git_sha[:8] or "-"; tag "-" when absent; host `https://<host>`.
  - doctor: checks in exact order — registry parses (N services), cluster
    reachable (early-return), node ready, cloudflared/gatus/sos-dashboard
    running, builder image (SKIP when crictl unavailable), tunnel healthy,
    tunnel ingress vs registry, secrets/<svc>.env (KEY NAMES only — values
    never read into memory beyond presence), deployed/<svc> (live image ==
    recorded intent), parity/<svc>, lifecycle warns, cname/<svc>.
    Marks `ok  `/`WARN`/`FAIL`/`SKIP`; footer `DOCTOR: all clear[ (N
    warning(s))]` / `N problem(s) found`; rc=1 on any fail. `--json`
    emits the same check records.
- Cloudflare: token read from <lab_root>/secrets/cloudflare.env
  (CF_API_TOKEN), used transiently in-process, never printed or journaled.
- ZERO mutations: only read verbs; no dns --apply, no monitor, no deploy.
- Registry v2 YAML is read by a bounded stdlib subset parser (2-space
  indent maps/lists, flow lists, quoted scalars) validated against
  fixtures AND the live file; state files via encoding/json.

## Pieces (each: verify → journal → integrate; corpus green required)

1. ops/SITES.yaml + site parser + `site list` + C12a.
2. Contract readers + fixtures tests.
3. `ops status` + live parity diff vs `./lab status` (journaled).
4. `ops doctor` (+--json) + live parity diff vs `./lab doctor` (journaled)
   + C12b fixtures / C12c formatter / C12d rule-7 asserts + full corpus.

## Standing guardrails

- sos-lab untouched except running its READ-ONLY verbs (status/doctor);
  secrets never printed; VM left running; no mutations anywhere.

## Measured results (all verified this session, journaled as # verify lines)

| Piece | Verify | Result |
|---|---|---|
| 1 site model | C12a + corpus | SITE LIST renders; access modes validated; 28u/324a fail=0 |
| 2 contract readers | fixtures + corpus | registry v2 validation mirrors registry.py; secret values never read/printed |
| 3 ops status parity | diff vs ./lab status (live) | **byte-identical back-to-back** (first diff was kubectl AGE rounding between runs) |
| 4 ops doctor parity + C12b-d | diff vs ./lab doctor (live) | text **identical**; JSON **identical** after cname detail fix; DOCTOR: all clear rc=0; corpus 31u/347a fail=0 |

Rule-7 centerpiece verified with teeth: the ops runner passes ONLY
KUBECONFIG + a temp HOME to kubectl (C12b fake fails on any ambient var;
bash-injected _/PWD/SHLVL allowed), in-cluster access refuses outside the
declared environment, and the kubectl HOME fix removed .kube cache litter
from the working directory.
