#!/usr/bin/env bash
# C4a — deployment block + infra manifest static analysis.
# Offline & cluster-free. Asserts: block syntax, deploy_pin wiring, dry-run
# determinism with no mutation, and that every manifest passes a real
# `kubectl apply --dry-run=client` (client-side validation, no server).
# SKIPs the kubectl tier only when no kubectl binary exists.

# shellcheck source=scripts/lib.sh
source "$FLEET_ROOT/scripts/lib.sh"
# shellcheck source=../toolchain.env
source "$FLEET_ROOT/toolchain.env"
# shellcheck source=scripts/blocks/04-deploy.sh
source "$FLEET_ROOT/scripts/blocks/04-deploy.sh"

BLOCK="$FLEET_ROOT/scripts/blocks/04-deploy.sh"
INFRA="$FLEET_ROOT/infra/k8s"

assert_rc "deploy block bash -n" 0 bash -n "$BLOCK"
assert_eq "deploy_pin strips v" "${TOOLCHAIN_KUBECTL_VERSION#v}" "$(deploy_pin)"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# ── dry-run contract ────────────────────────────────────────────────────────
dry1="$(apply_manifests "$INFRA" --dry-run 2>&1)"
dry2="$(apply_manifests "$INFRA" --dry-run 2>&1)"

assert_contains "dry-run names pinned kubectl" \
  "[deploy][dry-run] using kubectl v${TOOLCHAIN_KUBECTL_VERSION#v}" "$dry1"
for f in 00-namespace.yaml 10-fleetctl-deploy.yaml 20-fleetctl-svc.yaml; do
  assert_contains "dry-run lists $f" "would apply $f" "$dry1"
done
assert_eq "dry-run byte-identical" "$dry1" "$dry2"
assert_not_file "dry-run writes nothing" "$scratch/probe"

# ── real client-side validation of every manifest ──────────────────────────
count="$(manifest_files "$INFRA" | wc -l | tr -d ' ')"
assert_eq "three manifests present" 3 "$count"

if command -v kubectl >/dev/null 2>&1; then
  # kubectl 1.36 contacts the API server even for --dry-run=client
  # (openapi + RESTMapper discovery) — there is no fully offline
  # validation path. The tier is honest about it: validate for real
  # when an API server answers, SKIP (never false-green) on offline
  # hosts — same discipline as the no-kubectl tier below.
  if kubectl get --raw=/readyz >/dev/null 2>&1; then
    while IFS= read -r mf; do
      base="$(basename "$mf")"
      out="$(kubectl apply --dry-run=client -f "$mf" 2>&1)"
      rc=$?
      if [[ $rc -eq 0 ]]; then
        report_pass "kubectl validates $base"
      else
        report_fail "kubectl validates $base" "rc=$rc :: $(printf '%s' "$out" | tail -1)"
      fi
    done < <(manifest_files "$INFRA")
  else
    report_skip "kubectl validates manifests" "no cluster API reachable (offline host)"
  fi
else
  report_skip "kubectl validates manifests" "no kubectl binary on PATH"
fi

finalize
