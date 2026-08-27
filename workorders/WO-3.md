---
wo: WO-3
title: Tier-1 VM drill (Ubuntu server emulated on this host)
status: EXECUTED
plan: PLAN.md
pieces:
  - id: 1
    title: vm-tier scripts + test-onvm.sh + C7a wiring unit
    verify: scripts/test-onvm.sh --with-vm (VM_DRILL_OK ALL_TIERS_GREEN)
    integrated: true
---

# WO-3 — Tier-1 VM drill: Ubuntu server emulated on this very host

> **Status:** EXECUTED this session · Owner directive: "emulate an ubuntu
> server on this vm" + "keep VM, regular updates".
>
> Constraint stack: uid 1000, NO sudo, NO /dev/kvm, cgroup v2 read-only
> (nested containers impossible), 12 host cores, ~25 GB free RAM.

## Solution delivered (scripts/vm-tier/ + scripts/test-onvm.sh)

1. **Unprivileged QEMU 10.0.11 vendor prefix** — full dependency closure
   (90 debs incl. glibc) downloaded from Debian trixie via apt directory
   overrides + `dpkg -x` into `.vm/qroot`. Zero system writes.
2. **Ubuntu 24.04.4 noble-minimal cloud image** (253 MB, sha256-verified
   against published SHA256SUMS) + pristine qcow2 overlay per drill.
3. **Cloud-init via HTTP NoCloud seed** (smbios ds=nocloud-net → host
   python http.server on 127.0.0.1:18080, guest reaches 10.0.2.2) — no ISO
   tooling needed.
4. **UEFI boot** (OVMF 4M pflash) because the minimal image is GPT/EFI-only.

## Live-fire results (measured)

| Phase | Result |
|---|---|
| guest boot + cloud-init | CLOUD_INIT_DONE after ~94 s |
| SSH (key-injected fleet user) | OK |
| `systemctl is-system-running` | **running** |
| k3s install (`get.k3s.io`, pinned v1.36.3) | **fleet-vm Ready control-plane v1.36.3+k3s1** — pin match |
| approvals → gated promote built→dev→stage→prod | refused shortcuts, then walked; units re-ran per hop |
| block 04 live apply vs guest API (hostfwd 16443) | namespace/deployment/service created |
| pod log | **`fleetctl 1.27.0`** — deterministic binary ran in the real cluster |
| rollback drill | bad tag `:drill-bad` → `rollout undo` → recovered `:local` |

## Hard-won lessons (encoded into scripts + C7a asserts)

- TCG entropy starvation: sshd/keygen hung the guest in cloud-init config
  phase → **`-object rng-random + virtio-rng-pci` is mandatory** (C7a asserts it).
- QEMU needs `-L` rom search paths (kvmvapic/vgabios live in extracted share).
- The smbios seed hint must be present or cloud-init provisions nothing.
- Ambient kubeconfig danger: an env without explicit KUBECONFIG fell back to
  the REAL hk-03-dev cluster — AGENTS.md now forbids ambient cluster creds.

## Machine contract

`scripts/test-onvm.sh` (stub, default): `VM_DRILL_OK phase=0 …`
`scripts/test-onvm.sh --with-vm`: full drill, ends `VM_DRILL_OK phase=done ALL_TIERS_GREEN`
