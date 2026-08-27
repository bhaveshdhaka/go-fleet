---
wo: WO-6
title: Distribution
status: EXECUTED
plan: PLAN.md
pieces:
  - id: 1
    title: VERSION 0.1.0 + stamp through build-fleet.sh + MIT LICENSE
    verify: go vet + go test ./internal/fleet + bash scripts/test.sh
    integrated: true
  - id: 2
    title: static release build (linux/darwin) + SHA256SUMS + C11a
    verify: bash scripts/test.sh (C11a green + full corpus)
    integrated: true
  - id: 3
    title: installer path in install.sh + C11b
    verify: bash scripts/test.sh (C11b green + full corpus)
    integrated: true
  - id: 4
    title: gh repo create bhaveshdhaka/go-fleet + push (DEFERRED by owner)
    verify: gh repo view bhaveshdhaka/go-fleet
    integrated: false
---

# WO-6 — Distribution: license, version stamp, release artifacts, repo

> **Status:** EXECUTED this session · Owner directive: execute PLAN.md
> WO-6 (goal message 2026-08-27). Piece 4 (gh repo create) DEFERRED by
> owner directive "just do locally sync later" (2026-08-27): no gh binary
> and no token exist in the container; the remote sync happens when the
> owner provides gh auth. Everything local landed green.

## Plan section (decisions, dated 2026-08-27)

- Version source: repo-root `VERSION` file (`0.1.0`), stamped by
  ci/build-fleet.sh via -X main.version. Apps keep their block-03
  toolchain pin stamp (separate contract). C9a assert moves from
  TOOLCHAIN_GO_VERSION to VERSION.
- Release artifacts: ci/build-release.sh — CGO_ENABLED=0, static,
  GOOS/GOARCH matrix linux/amd64 + darwin/amd64 + darwin/arm64, same
  trimpath/no-vcs determinism flags, output dist/release/fleet_<os>_<arch>
  + SHA256SUMS; repeat builds byte-identical (asserted).
- Installer path: install.sh builds the fleet CLI into the toolchain
  prefix ($prefix/bin/fleet) after go verification, reported in the
  existing machine contract without changing FLEET_INSTALL_OK format.
- gh step: `gh repo create bhaveshdhaka/go-fleet` + push — executed the
  moment the owner provides gh auth (binary + token); everything else in
  this workorder is credential-free and lands first.

## Pieces (each: verify → journal → integrate; corpus green required)

1. VERSION + stamp + LICENSE. verify: go vet + go test + corpus.
2. Release builder + SHA256SUMS + C11a. verify: corpus.
3. install.sh fleet path + C11b. verify: corpus.
4. gh repo create + push. verify: gh repo view. **BLOCKED — needs owner
   gh auth (no gh binary, no token in container).**

## Standing guardrails

- QEMU drill VM left running; sos-lab authoritative for hk-03-dev until
  WO-8 parity; zero cluster operations in WO-6; no secrets in repo files.
