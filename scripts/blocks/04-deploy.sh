#!/usr/bin/env bash
# fleet block 04 — k8s deployment.
#
# Applies the infra/k8s manifests with the pinned kubectl from
# toolchain.env (single source of truth). Deterministic --dry-run prints
# the exact plan without mutating anything and without contacting a
# cluster; real apply requires a reachable context (VM tier).
#
# Usage:
#   source blocks/04-deploy.sh
#   apply_manifests <infra-dir> [--dry-run]

set -uo pipefail
# shellcheck disable=SC1091
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/toolchain.env"

# deploy_pin -> exact kubectl version the deploy step must run under.
deploy_pin() { printf '%s' "$TOOLCHAIN_KUBECTL_VERSION" | sed 's/^v//'; }

# manifest_files <infra-dir> -> sorted list of .yaml files (deterministic order)
manifest_files() {
  local dir=$1
  find "$dir" -maxdepth 1 -name '*.yaml' -type f | LC_ALL=C sort
}

# apply_manifests <infra-dir> [--dry-run]
apply_manifests() {
  local infra_dir=$1 dry_run=false
  shift
  for a in "$@"; do [[ "$a" == "--dry-run" ]] && dry_run=true; done

  [[ -d $infra_dir ]] || { echo "ERROR: infra dir '$infra_dir' missing" >&2; return 1; }
  local files want n
  files="$(manifest_files "$infra_dir")"
  n="$(printf '%s\n' "$files" | grep -c .)"
  want="v$(deploy_pin)"

  if $dry_run; then
    echo "[deploy][dry-run] using kubectl $want"
    echo "[deploy][dry-run] apply $n manifest(s) from ${infra_dir}"
    local f
    while IFS= read -r f; do
      echo "[deploy][dry-run] would apply $(basename "$f")"
    done <<< "$files"
    return 0
  fi

  echo "[deploy] applying with kubectl $want ..."
  local f
  for f in $files; do
    kubectl apply -f "$f"
  done
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  INFRA="${1:?usage: $0 <infra-dir> [--dry-run]}"
  shift
  apply_manifests "$INFRA" "$@"
fi
