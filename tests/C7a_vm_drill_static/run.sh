#!/usr/bin/env bash
# C7a — Tier-1 VM drill wiring (STUB tier only: never boots a VM here).
# Asserts the vm-tier scripts are syntax-clean, the drill machine contract
# is present, vendor prefix rules are gitignored, and the UEFI/rng flags
# that fixed real boot failures are encoded in up.sh. Zero mutation.

source "$FLEET_ROOT/scripts/lib.sh"

for s in fetch-qemu fetch-image up down build-image-tar; do
  assert_rc "vm-tier $s.sh syntax" 0 bash -n "$FLEET_ROOT/scripts/vm-tier/$s.sh"
done
assert_rc "test-onvm.sh syntax" 0 bash -n "$FLEET_ROOT/scripts/test-onvm.sh"

drill="$(cat "$FLEET_ROOT/scripts/test-onvm.sh")"
for kw in "VM_DRILL_OK" "VM_DRILL_FAIL" "VM_DRILL_SKIP"; do
  assert_contains "drill emits $kw" "$kw" "$drill"
done

up="$(cat "$FLEET_ROOT/scripts/vm-tier/up.sh")"
assert_contains "up.sh uses UEFI pflash" "if=pflash" "$up"
grep -q "virtio-rng-pci\|rng-random" "$FLEET_ROOT/scripts/vm-tier/up.sh" \
  && report_pass "boot encodes virtio-rng entropy fix" \
  || report_fail "boot encodes virtio-rng entropy fix" "missing — TCG boot will hang on ssh keygen"

assert_contains "seed uses nocloud-net smbios" "ds=nocloud-net" "$up"

# .vm/ must never reach git (vendor prefix + images can exceed 2GB)
grep -qE '^\.vm/' "$FLEET_ROOT/.gitignore" \
  && report_pass ".vm/ gitignored" \
  || report_fail ".vm/ gitignored" "vendor prefix risk"

# image-tar builder produces a valid docker-archive skeleton (pure files)
scratch="$(mktemp -d)"; trap 'rm -rf "$scratch"' EXIT
bin="$scratch/fakefleetctl"
printf '#!/usr/bin/env bash\necho ok\n' >"$bin"; chmod +x "$bin"
if bash "$FLEET_ROOT/scripts/vm-tier/build-image-tar.sh" "$bin" "$scratch/o.tar" >/dev/null 2>&1 \
   && tar -tf "$scratch/o.tar" | grep -q "^manifest.json$" \
   && tar -xf "$scratch/o.tar" -C "$scratch" manifest.json \
   && grep -q '"Layers":\["layer.tar"\]' "$scratch/manifest.json"; then
  report_pass "image-tar skeleton valid"
else
  report_fail "image-tar skeleton valid" "missing/malformed manifest.json"
fi

finalize
