#!/usr/bin/env bash
# scripts/vm-tier/up.sh — boot the drill VM headless and wait for SSH.
#
# Usage: scripts/vm-tier/up.sh [--fresh]
#   --fresh  recreate overlay + bump instance-id (true first boot)
# Starts: cloud-init HTTP seed server (127.0.0.1:18080) + QEMU (UEFI, TCG,
# hostfwd 127.0.0.1:22222 -> guest :22). Polls in short slices; machine
# lines: VM_UP ssh_port=<port> pid=<pid>  |  VM_UP_FAIL reason=<...>

set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
T="$ROOT/.vm"
FRESH=false
[[ "${1:-}" == "--fresh" ]] && FRESH=true

export LD_LIBRARY_PATH="$T/qroot/usr/lib/x86_64-linux-gnu:$T/qroot/lib/x86_64-linux-gnu"
QBIN="$T/qroot/usr/bin/qemu-system-x86_64"
SSH_PORT="${FLEET_VM_SSH_PORT:-22222}"
SEED_PORT="${FLEET_VM_SEED_PORT:-18080}"

[[ -x "$QBIN" ]] || { echo "VM_UP_FAIL reason=run fetch-qemu.sh first"; exit 1; }
[[ -f "$T/img/disk.qcow2" ]] || { echo "VM_UP_FAIL reason=run fetch-image.sh first"; exit 1; }

if $FRESH; then
  rm -f "$T/img/disk.qcow2"
  "$T/qroot/usr/bin/qemu-img" create -f qcow2 -F qcow2 \
      -b "$T/img/base.qcow2" "$T/img/disk.qcow2" 20G >/dev/null
  printf 'instance-id: fleet-drill-%s\n' "$(date +%s)" > "$T/seed/meta-data"
fi

# stop any previous run of OUR qemu (match on this exact pidfile chain)
if [[ -f "$T/run/qemu.pid" ]] && kill -0 "$(cat "$T/run/qemu.pid")" 2>/dev/null; then
  kill "$(cat "$T/run/qemu.pid")" 2>/dev/null; sleep 2
fi
curl -fsS -m 1 "http://127.0.0.1:$SEED_PORT/meta-data" >/dev/null 2>&1 \
  || { nohup python3 -m http.server "$SEED_PORT" --bind 127.0.0.1 \
         --directory "$T/seed" >/dev/null 2>&1 & sleep 1; }

: > "$T/run/console.log"
[[ -f "$T/run/vars.fd" ]] || cp "$T/qroot/usr/share/OVMF/OVMF_VARS_4M.snakeoil.fd" "$T/run/vars.fd"

nohup "$QBIN" \
  -name fleet-drill -machine q35 -accel tcg,thread=multi \
  -L "$T/qroot/usr/share/qemu" -L "$T/qroot/usr/share/seabios" \
  -smp "${FLEET_VM_SMP:-4}" -m "${FLEET_VM_MEM_MB:-4096}" -cpu max \
  -object rng-random,filename=/dev/urandom,id=rng0 \
  -device virtio-rng-pci,rng=rng0 \
  -drive if=pflash,format=raw,readonly=on,file="$T/qroot/usr/share/OVMF/OVMF_CODE_4M.fd" \
  -drive if=pflash,format=raw,file="$T/run/vars.fd" \
  -drive file="$T/img/disk.qcow2",if=virtio,format=qcow2 \
  -netdev user,id=n0,hostfwd=tcp:127.0.0.1:$SSH_PORT-:22 \
  -device virtio-net-pci,netdev=n0 \
  -smbios type=1,serial=ds=nocloud-net\;s=http://10.0.2.2:$SEED_PORT/ \
  -display none -serial file:"$T/run/console.log" \
  > "$T/run/qemu.out" 2>&1 &
pid=$!; echo "$pid" > "$T/run/qemu.pid"
sleep 5
kill -0 "$pid" 2>/dev/null || { echo "VM_UP_FAIL reason=qemu_died:$(tail -1 $T/run/qemu.out)"; exit 1; }

ssh_up() {
  timeout 2 bash -c "exec 3<>/dev/tcp/127.0.0.1/$SSH_PORT && head -c 24 <&3" 2>/dev/null | grep -q SSH
}

# short-slice wait: ~20 min ceiling, printed progress every ~30s
for i in $(seq 1 40); do
  if ssh_up; then
    echo "VM_UP ssh_port=$SSH_PORT pid=$pid"
    exit 0
  fi
  sleep 30
  kill -0 "$pid" 2>/dev/null || { echo "VM_UP_FAIL reason=qemu_died_mid_boot"; exit 1; }
done
echo "VM_UP_FAIL reason=ssh_timeout_20min"
exit 1
