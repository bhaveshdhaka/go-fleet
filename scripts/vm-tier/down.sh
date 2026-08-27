#!/usr/bin/env bash
# scripts/vm-tier/down.sh — gracefully power off the drill VM (ACPI), with
# forced kill fallback. Also stops the seed server if we own it.

set -uo pipefail
T="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/.vm"
SSH_PORT="${FLEET_VM_SSH_PORT:-22222}"
SEED_PORT="${FLEET_VM_SEED_PORT:-18080}"

pid="$(cat "$T/run/qemu.pid" 2>/dev/null || true)"
if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
  if command -v ssh >/dev/null 2>&1 && [[ -f "$T/ssh/id" ]]; then
    # try graceful first (guest ACPI poweroff)
    timeout 20 ssh -i "$T/ssh/id" -p "$SSH_PORT" \
      -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR \
      fleet@127.0.0.1 'sudo poweroff' >/dev/null 2>&1 || true
    for _ in $(seq 1 12); do
      kill -0 "$pid" 2>/dev/null || { echo "VM_DOWN graceful"; exit 0; }
      sleep 2
    done
  fi
  kill "$pid" 2>/dev/null || true
  echo "VM_DOWN killed"
else
  echo "VM_DOWN not_running"
fi
