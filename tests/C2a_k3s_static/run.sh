#!/usr/bin/env bash
# C2a — k3s block static analysis.
# Verifies 01-k3s.sh syntax, k3s_pin wiring, dry-run determinism, and guard
# clause no-op — all without network or a real k3s daemon.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"

BLOCK="$FLEET_ROOT/scripts/blocks/01-k3s.sh"

# ── 1. syntax check ────────────────────────────────────────────────────────
assert_rc "k3s block bash -n" 0 bash -n "$BLOCK"

# ── 2. k3s_pin returns version without leading 'v' ─────────────────────────
source "$FLEET_ROOT/toolchain.env"
# shellcheck source=scripts/blocks/01-k3s.sh
source "$BLOCK"
pin="$(k3s_pin)"
assert_eq "k3s_pin strips leading v" "${TOOLCHAIN_KUBECTL_VERSION#v}" "$pin"

# ── 3. dry-run determinism & no mutation ────────────────────────────────────
state_root="$(mktemp -d)"
state_dir="$state_root/k3s-state"

out1="$(bring_up_k3s "$state_dir" --dry-run)"
out2="$(bring_up_k3s "$state_dir" --dry-run)"

assert_contains "dry-run names pinned version" "[k3s][dry-run] would install k3s v${pin}" "$out1"
assert_eq "dry-run byte-identical" "$out1" "$out2"
assert_not_file "dry-run does not create state-dir" "$state_dir"

# ── 4. guard clause: k3s + kubectl both succeed → no-op ────────────────────
guard_dir="$(mktemp -d)"
fake_bin="$guard_dir/bin"
mkdir -p "$fake_bin"

cat > "$fake_bin/k3s" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat > "$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$fake_bin/k3s" "$fake_bin/kubectl"

PATH="$fake_bin:$PATH" noop_out="$(bring_up_k3s "$guard_dir/state" --dry-run)"
assert_contains "guard reports no-op" "already running" "$noop_out"

rm -rf "$state_root" "$guard_dir"

finalize
