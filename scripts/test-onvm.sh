#!/usr/bin/env bash
# scripts/test-onvm.sh — Tier-1 drill: real Ubuntu VM via userspace QEMU.
#
# Modes:
#   scripts/test-onvm.sh            STUB mode (default): hermetic checks that
#                                   the tier is wired — vendored qemu runs,
#                                   image present, scripts syntax-clean,
#                                   machine lines correct. No VM is booted.
#   scripts/test-onvm.sh --with-vm  REAL drill: ensure fetch+boot, then
#                                   remote-execute the live phases:
#                                     1. systemd + kernel namespaces sanity
#                                     2. block 01 REAL k3s bring-up (needs net)
#                                     3. kubectl get nodes == Ready
#                                     4. fleetctl promoted to prod + applied
#                                        against the live cluster (block 04)
#                                     5. rollback smoke (kubectl rollout undo)
# Machine lines: VM_DRILL_OK phase=<n> | VM_DRILL_FAIL phase=<n> reason=<...>

set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
T="$ROOT/.vm"
SSH_PORT="${FLEET_VM_SSH_PORT:-22222}"
export FLEET_ROOT="$ROOT"
# shellcheck disable=SC1091
source "$ROOT/toolchain.env"
export LD_LIBRARY_PATH="$ROOT/.vm/qroot/usr/lib/x86_64-linux-gnu:$ROOT/.vm/qroot/lib/x86_64-linux-gnu${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

vt() { "$ROOT/scripts/vm-tier/$1" "${@:2}"; }

ssh_vm() { # ssh_vm <cmd...>
  timeout "${FLEET_VM_SSH_TIMEOUT:-120}" ssh -i "$T/ssh/id" -p "$SSH_PORT" \
    -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -o LogLevel=ERROR -o ConnectTimeout=5 fleet@127.0.0.1 "$@"
}

fail() { echo "VM_DRILL_FAIL phase=$1 reason=$2"; exit 1; }

# ---------------------------------------------------------------- stub mode --
run_stub() {
  [[ -x "$T/qroot/usr/bin/qemu-system-x86_64" ]] \
    || { echo "VM_DRILL_SKIP phase=0 reason=qemu not vendored (run vm-tier/fetch-qemu.sh)"; exit 0; }
  ver="$("$T/qroot/usr/bin/qemu-system-x86_64" --version | awk 'NR==1{print $4}')"
  [[ -n "$ver" ]] || fail 0 "vendored qemu not executable"
  echo "VM_DRILL_OK phase=0 qemu=$ver"
  for s in fetch-qemu fetch-image up down; do
    bash -n "$ROOT/scripts/vm-tier/$s.sh" || fail 0 "syntax $s.sh"
  done
  echo "VM_DRILL_OK phase=0.5 tier_scripts_syntax"
  [[ -f "$T/img/base.qcow2" ]] && echo "VM_DRILL_OK phase=0.6 image_cached" \
                              || echo "VM_DRILL_NOTE image not cached yet"
  exit 0
}

[[ "${1:-}" == "--with-vm" ]] || run_stub

# ---------------------------------------------------------------- real mode --
[[ -x "$T/qroot/usr/bin/qemu-system-x86_64" ]] || vt fetch-qemu.sh >/dev/null \
  || fail 1 "fetch-qemu"
[[ -f "$T/img/disk.qcow2" ]] || vt fetch-image.sh >/dev/null || fail 1 "fetch-image"

up_out="$(vt up.sh 2>&1)" || fail 2 "boot: $up_out"
echo "$up_out"

for _ in $(seq 1 60); do
  ssh_vm 'echo READY' 2>/dev/null | grep -q READY && break
  sleep 5
done
ssh_vm 'echo READY' 2>/dev/null | grep -q READY || fail 2 "ssh never accepted"

# phase 1: guest sanity — systemd genuinely functional, namespaces available
ssh_vm 'systemctl is-system-running || systemctl is-system-running --quiet; \
        cat /proc/sys/user/max_user_namespaces; uname -r' \
  | tee "$T/run/guest-sanity.txt" >/dev/null \
  || fail 3 "guest sanity probes failed"
grep -qE '^(running|degraded)' "$T/run/guest-sanity.txt" \
  || fail 3 "systemd not running"
echo "VM_DRILL_OK phase=3 guest_systemd_and_kernel"

# phase 4: REAL k3s bring-up — idempotent: skip install if daemon already active
k3s_version="v$(sed -n 's/^export TOOLCHAIN_KUBECTL_VERSION="v*//p' "$ROOT/toolchain.env" | head -1)"
if ! ssh_vm 'systemctl is-active k3s' 2>/dev/null | grep -q active; then
  ssh_vm "curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='$k3s_version' \
          INSTALL_K3S_EXEC='--disable=traefik --write-kubeconfig-mode 644' sh -" \
    >/dev/null || fail 4 "k3s install script failed"
fi
for _ in $(seq 1 30); do
  ssh_vm 'systemctl is-active k3s' 2>/dev/null | grep -q active && break
  sleep 8
done
ssh_vm 'systemctl is-active k3s' 2>/dev/null | grep -q active || fail 4 "k3s not active"
node_ver="$(ssh_vm 'sudo k3s kubectl get nodes -o jsonpath={.items[0].status.nodeInfo.kubeletVersion}')"
[[ "$node_ver" == "$k3s_version+k3s1" ]] \
  || fail 4 "kubelet version $node_ver != pinned $k3s_version+k3s1"
echo "VM_DRILL_OK phase=4 k3s_version=$node_ver"

# kubeconfig handoff: host kubectl -> guest API (port 16443 -> 6443)
ssh_vm 'sudo cp /etc/rancher/k3s/k3s.yaml /tmp/k3s.yaml && sudo chmod 644 /tmp/k3s.yaml'
scp -q -i "$T/ssh/id" -P "$SSH_PORT" \
    -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR \
    fleet@127.0.0.1:/tmp/k3s.yaml "$T/run/kubeconfig" \
  || fail 4 "kubeconfig scp failed"
sed -i 's|server: https://127.0.0.1:6443|server: https://127.0.0.1:16443|' "$T/run/kubeconfig"
chmod 600 "$T/run/kubeconfig"
export KUBECONFIG="$T/run/kubeconfig"
kubectl get nodes >/dev/null 2>&1 || fail 4 "host kubectl cannot reach guest API"

# phase 5: live apply (block 04 real branch) + verify + rollback drill
scratch="$(mktemp -d)"; trap 'rm -rf "$scratch"' EXIT
bash "$ROOT/scripts/blocks/03-pipeline.sh" fleetctl "$scratch/dist" >/dev/null \
  || fail 5 "deterministic build failed"
bash "$ROOT/scripts/vm-tier/build-image-tar.sh" "$scratch/dist/fleetctl" "$scratch/fleetctl.tar" \
  || fail 5 "image tar failed"
scp -q -i "$T/ssh/id" -P "$SSH_PORT" \
    -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR \
    "$scratch/fleetctl.tar" fleet@127.0.0.1:/tmp/fleetctl.tar || fail 5 "image scp failed"
ssh_vm 'sudo k3s ctr images import /tmp/fleetctl.tar >/dev/null' || fail 5 "ctr import failed"

bash "$ROOT/scripts/blocks/04-deploy.sh" "$ROOT/infra/k8s" >/dev/null \
  || fail 5 "block04 apply failed"
for _ in $(seq 1 24); do
  st="$(kubectl get pods -n fleet-lab --no-headers 2>/dev/null | awk '{print $3}')"
  [[ "$st" == "Completed" || "$st" == "Running" ]] && break
  sleep 5
done
kubectl logs -n fleet-lab deploy/fleetctl 2>/dev/null | grep -q "fleetctl $TOOLCHAIN_GO_VERSION" \
  || fail 5 "pod log does not show pinned-version banner"
echo "VM_DRILL_OK phase=5 live_apply_and_binary_verified"

# rollback drill: bad image tag -> rollout fails -> undo -> recovered
kubectl -n fleet-lab set image deploy/fleetctl fleetctl=localhost/fleet/fleetctl:drill-bad \
  >/dev/null || fail 6 "set image failed"
kubectl -n fleet-lab rollout undo deploy/fleetctl >/dev/null 2>&1 || fail 6 "undo failed"
img="$(kubectl -n fleet-lab get deploy fleetctl \
        -o jsonpath='{.spec.template.spec.containers[0].image}')"
[[ "$img" == "localhost/fleet/fleetctl:local" ]] \
  || fail 6 "rollback did not restore known-good image"
echo "VM_DRILL_OK phase=6 rollback_restored_known_good"

echo "VM_DRILL_OK phase=done ALL_TIERS_GREEN"
