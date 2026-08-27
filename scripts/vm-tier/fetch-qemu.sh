#!/usr/bin/env bash
# scripts/vm-tier/fetch-qemu.sh — unprivileged QEMU 10 vendor prefix.
#
# Downloads the full dependency closure of qemu-system-x86 (+ openssh-client,
# qemu-utils, ovmf) from the Debian trixie suite using apt directory
# overrides — NO ROOT, NO SYSTEM CHANGES — then dpkg -x extracts everything
# into .vm/qroot. Rerunnable: cached, byte-verified by apt sums.
#
# Outputs (under repo/.vm/): qroot/{usr/bin,usr/share}, apt/lists,
# img/, seed/, ssh/, run/. Machine line on success:
#   VM_FETCH_OK qemu=<version> root=<abs path>
# All downloads land inside the repo; .vm/ is gitignored.

set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
T="$ROOT/.vm"
SUITE="${FLEET_VM_SUITE:-trixie}"

mkdir -p "$T/apt/lists" "$T/apt/cache/archives/partial" "$T/debs" \
         "$T/img" "$T/seed" "$T/ssh" "$T/run" "$T/qroot"
touch "$T/apt/status"

cat > "$T/apt/sources.list" <<EOF
deb [trusted=yes] http://deb.debian.org/debian $SUITE main
EOF

apt_get() {
  apt-get -o Dir::State::Lists=$T/apt/lists \
          -o Dir::Cache=$T/apt/cache \
          -o Dir::Etc::SourceList=$T/apt/sources.list \
          -o Dir::Etc::SourceParts=/dev/null \
          -o Dir::State::status=$T/apt/status \
          -o Debug::NoLocking=1 \
          -o Acquire::Languages=none "$@"
}

apt_get update >/dev/null 2>&1
apt_get --no-install-recommends -y --download-only \
    install qemu-system-x86 openssh-client qemu-utils ovmf >/dev/null 2>&1

n=0
for d in "$T"/apt/cache/archives/*.deb; do
  dpkg -x "$d" "$T/qroot" && n=$((n + 1))
done
[[ $n -gt 0 ]] || { echo "VM_FETCH_FAIL reason=no_debs_extracted"; exit 1; }

export LD_LIBRARY_PATH="$T/qroot/usr/lib/x86_64-linux-gnu:$T/qroot/lib/x86_64-linux-gnu"
ver="$("$T/qroot/usr/bin/qemu-system-x86_64" --version 2>/dev/null | head -1)"
[[ -n "$ver" ]] || { echo "VM_FETCH_FAIL reason=qemu_binary_won't_run"; exit 1; }
echo "VM_FETCH_OK qemu=$ver root=$T/qroot debs=$n"
