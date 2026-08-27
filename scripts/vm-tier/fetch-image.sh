#!/usr/bin/env bash
# scripts/vm-tier/fetch-image.sh — pinned Ubuntu cloud image + pristine overlay.
#
# Downloads the noble-minimal cloud image once (sha256-verified against the
# published SHA256SUMS), then creates a throwaway qcow2 OVERLAY so every
# drill starts from the pristine base. Rerunnable.

set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
T="$ROOT/.vm"
BASE_URL="${FLEET_VM_IMAGE_BASE:-https://cloud-images.ubuntu.com/minimal/releases/noble/release}"
IMG="${FLEET_VM_IMAGE_FILE:-ubuntu-24.04-minimal-cloudimg-amd64.img}"

mkdir -p "$T/img" "$T/seed" "$T/ssh" "$T/run"

if [[ ! -f "$T/img/base.qcow2" ]]; then
  echo "[vm-image] downloading $IMG ..."
  curl -fsSL -o "$T/img/base.qcow2" "$BASE_URL/$IMG" \
    || { echo "VM_IMAGE_FAIL reason=download"; exit 1; }
fi

# integrity: compare against published SHA256SUMS (pinned suite)
sums="$(curl -fsSL "$BASE_URL/SHA256SUMS" 2>/dev/null | grep "$IMG" | awk '{print $1}')"
if [[ -n "$sums" ]]; then
  got="$(sha256sum "$T/img/base.qcow2" | awk '{print $1}')"
  if [[ "$got" != "$sums" ]]; then
    echo "VM_IMAGE_FAIL reason=sha256_mismatch expected=$sums got=$got"
    exit 1
  fi
  echo "[vm-image] sha256 OK"
else
  echo "[vm-image] WARN: SHA256SUMS unreachable; proceeding with unverified base"
fi

export LD_LIBRARY_PATH="$T/qroot/usr/lib/x86_64-linux-gnu:$T/qroot/lib/x86_64-linux-gnu"
"$T/qroot/usr/bin/qemu-img" create -f qcow2 -F qcow2 \
    -b "$T/img/base.qcow2" "$T/img/disk.qcow2" "${FLEET_VM_DISK_SIZE:-20G}" >/dev/null

echo "VM_IMAGE_OK base=$T/img/base.qcow2 overlay=$T/img/disk.qcow2"
