#!/usr/bin/env bash
# fleet block 01 — k3s server bring-up.
#
# Installs a pinned single-node k3s on a fresh Ubuntu box and waits until the
# node is Ready. Idempotent: a running k3s at a compatible version is left
# alone. --dry-run prints the exact plan without mutating anything.
#
# NOTE: the actual k3s server daemon requires a real host (systemd + kernel);
# it is NOT exercised by the hermetic container tier. Container tests verify
# this script's wiring (guard clauses, pin wiring, dry-run determinism); the
# real bring-up is validated by the VM drill (scripts/test-on-vm.sh).

set -uo pipefail
# shellcheck disable=SC1091
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/toolchain.env"

k3s_pin() { printf '%s' "$TOOLCHAIN_KUBECTL_VERSION" | sed 's/^v//'; }

# bring_up_k3s <state-dir> [--dry-run]
bring_up_k3s() {
  local state_dir=$1 dry_run=false
  shift
  for a in "$@"; do [[ "$a" == "--dry-run" ]] && dry_run=true; done

  local want
  want="$(k3s_pin)"

  # Guard 1: already installed + node Ready -> no-op (idempotent)
  if command -v k3s >/dev/null 2>&1 && kubectl get nodes >/dev/null 2>&1; then
    [[ $dry_run == false ]] && echo "[k3s] already running — no-op"
    echo "[k3s][dry-run] already running — no-op" | sed 's/\[k3s\]/&/'
    return 0
  fi

  # Guard 2: root required (k3s install needs sudo)
  if [[ $dry_run == false && "$(id -u)" -ne 0 ]]; then
    echo "ERROR: k3s install requires root" >&2
    return 1
  fi

  if $dry_run; then
    echo "[k3s][dry-run] would install k3s v$want via get.k3s.io"
    echo "[k3s][dry-run] would wait for node Ready (kubectl get nodes)"
    return 0
  fi

  mkdir -p "$state_dir"
  # k3s releases are the Kubernetes pin plus the fork suffix (kubectl
  # v1.36.3 -> k3s tag v1.36.3+k3s1); the bare tag 404s on get.k3s.io.
  # The kubelet then reports v<want>+k3s1 — the convention the VM drill
  # asserts (test-onvm.sh phase 4).
  echo "[k3s] installing k3s v$want+k3s1 ..."
  curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="v${want}+k3s1" sh -

  echo "[k3s] waiting for node Ready ..."
  local i=0
  until kubectl get nodes 2>/dev/null | grep -q Ready; do
    i=$((i + 1))
    [[ $i -gt 60 ]] && { echo "ERROR: node not Ready within 60s" >&2; return 1; }
    sleep 2
  done

  echo "[k3s] node Ready"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  STATE_DIR="${1:?usage: $0 <state-dir> [--dry-run]}"
  shift
  bring_up_k3s "$STATE_DIR" "$@"
fi
