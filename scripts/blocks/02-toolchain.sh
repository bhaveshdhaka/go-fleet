#!/usr/bin/env bash
# fleet block 02 — toolchain installer.
#
# Installs every pinned platform CLI into a self-contained prefix so the
# whole lab's tooling is reproducible and never pollutes the system. Reads
# versions from toolchain.env (single source of truth). Idempotent: safe to
# re-run; already-present tools at the pinned version are left untouched.
#
# Usage:
#   source blocks/02-toolchain.sh
#   install_toolchain "$PREFIX" [--dry-run] [--force]
#
# Design note (testability): install_toolchain has a --dry-run mode that
# prints exactly what it would execute (deterministic, no network). Tests
# assert the wiring + pins offline; real downloads happen only without
# --dry-run and are covered by the hermetic container tier.

set -uo pipefail
# shellcheck disable=SC1091
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/toolchain.env"

# tool_source <tool> -> prints a URL for a pinned binary tarball. Extend here
# for new tools. Deterministic per (tool, version, arch, os).
tool_source() {
  local tool=$1
  local v arch os
  v="$(pin "$tool")"
  arch="$TOOLCHAIN_ARCH"; os="$TOOLCHAIN_OS"
  case "$tool" in
    go)
      echo "https://go.dev/dl/go${v}.${os}-${arch}.tar.gz" ;;
    kubectl)
      echo "https://dl.k8s.io/release/${v}/bin/${os}/${arch}/kubectl" ;;
    restic)
      echo "https://github.com/restic/restic/releases/download/v${v}/restic_${v}_${os}_${arch}.bz2" ;;
    argocd)
      echo "https://github.com/argoproj/argo-cd/releases/download/${v}/argocd-${os}-${arch}" ;;
    kubeseal)
      echo "https://github.com/bitnami-labs/sealed-secrets/releases/download/${v}/kubeseal-${os}-${arch}" ;;
    dagger)
      echo "https://dl.dagger.io/dagger/install.sh" ;;
    templ|tailwindcss)
      # installed from GitHub release tarball; shape varies, handled in step()
      echo "https://github.com/$([[ $tool == templ ]] && echo 'a-h/templ' || echo 'tailwindlabs/tailwindcss')/releases/download/${v}/" ;;
    *)
      echo "ERROR: no source for tool '$tool'" >&2
      return 1 ;;
  esac
}

# pin <tool> -> exact version string from toolchain.env
pin() {
  local tool=$1 name
  case "$tool" in
    # toolchain.env keys its tailwind pin as TAILWIND, not TAILWINDCSS.
    tailwindcss) name="TAILWIND" ;;
    *)  name="$(printf '%s' "$tool" | tr '[:lower:]' '[:upper:]' | tr '-' '_')" ;;
  esac
  local val
  val="$(eval "printf '%s' \"\${TOOLCHAIN_${name}_VERSION:-}\"")"
  if [[ -z "$val" ]]; then
    echo "ERROR: no pin for '$tool'" >&2
    return 1
  fi
  printf '%s' "$val"
}

# bin_present <prefix> <tool> <version>
bin_present() {
  local prefix=$1 tool=$2 want=$3 bin="$2"
  [[ -x "$prefix/bin/$bin" ]] || return 1
  # Verify version matches pin (best-effort per tool). Compare v-stripped
  # values on BOTH sides: some pins carry a leading v, others do not.
  local got
  case "$tool" in
    go)     got="$("$prefix/bin/go" version | awk '{print $3}' | sed 's/^go//')" ;;
    kubectl) got="$("$prefix/bin/kubectl" version --client --output json 2>/dev/null \
              | sed -n 's/.*"gitVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)" ;;
    restic) got="$("$prefix/bin/restic" version | awk '{print $2}')" ;;
    argocd) got="$("$prefix/bin/argocd" version --client 2>/dev/null | awk 'NR==1{print $2}')" ;;
    kubeseal) got="$("$prefix/bin/kubeseal" --version 2>/dev/null | awk '{print $NF}')" ;;
    dagger) got="$("$prefix/bin/dagger" version 2>/dev/null | awk '{print $2}')" ;;
    templ)  got="$("$prefix/bin/templ" version 2>/dev/null | awk 'NR==1{print $2}')" ;;
    tailwindcss) got="$("$prefix/bin/tailwindcss" --help >/dev/null 2>&1 && echo skip)" ;;
    *)      got=skip ;;
  esac
  [[ "$got" == "skip" ]] && return 0
  [[ "$(v_strip "$got")" == "$(v_strip "$want")" ]] && return 0
  return 1
}

v_strip() { printf '%s' "$1" | sed 's/^v//'; }

# install_toolchain <prefix> [--dry-run] [--force]
install_toolchain() {
  local prefix=$1 dry_run=false force=false
  shift
  for a in "$@"; do
    [[ "$a" == "--dry-run" ]] && dry_run=true
    [[ "$a" == "--force" ]] && force=true
  done

  # arch/os consumed by the templ/go download cases; must exist under set -u.
  local arch="$TOOLCHAIN_ARCH" os="$TOOLCHAIN_OS"

  local tools=(go kubectl restic argocd kubeseal dagger templ tailwindcss)
  local errors=0 failed_tools=""
  local hints=""
  command -v bzip2 >/dev/null 2>&1 \
    || hints="[toolchain] hint: 'bzip2' missing on PATH — restic cannot be installed; everything else will proceed"

  if [[ $dry_run == false ]]; then
    mkdir -p "$prefix/bin" "$prefix/src" "$prefix/tmp"
    [[ -n "$hints" ]] && printf '%s\n' "$hints"
  fi

  for tool in "${tools[@]}"; do
    local v
    v="$(pin "$tool")" || return 1
    if ! $force && bin_present "$prefix" "$tool" "$v"; then
      [[ $dry_run == false ]] && echo "[toolchain] $tool already pinned ($v) — skip"
      continue
    fi

    if $dry_run; then
      echo "[toolchain][dry-run] install $tool=$v from $(tool_source "$tool")"
      continue
    fi

    echo "[toolchain] installing $tool=$v ..."
    case "$tool" in
      go)
        curl -fsSL "$(tool_source go)" | tar -xz -C "$prefix/src"
        ln -sf "$prefix/src/go/bin/go" "$prefix/bin/go"
        ln -sf "$prefix/src/go/bin/gofmt" "$prefix/bin/gofmt"
        ;;
      kubectl)
        curl -fsSL "$(tool_source kubectl)" -o "$prefix/bin/kubectl"
        chmod +x "$prefix/bin/kubectl"
        ;;
      restic)
        curl -fsSL "$(tool_source restic)" | bzip2 -dc > "$prefix/bin/restic"
        chmod +x "$prefix/bin/restic"
        ;;
      argocd)
        curl -fsSL "$(tool_source argocd)" -o "$prefix/bin/argocd"
        chmod +x "$prefix/bin/argocd"
        ;;
      kubeseal)
        curl -fsSL "$(tool_source kubeseal)" -o "$prefix/bin/kubeseal"
        chmod +x "$prefix/bin/kubeseal"
        ;;
      dagger)
        curl -fsSL "$(tool_source dagger)" | DAGGER_VERSION="$v" INSTALL_DIR="$prefix/bin" sh >/dev/null
        ;;
      templ)
        curl -fsSL "https://github.com/a-h/templ/releases/download/${v}/templ_${os}_${arch}.tar.gz" \
          | tar -xz -C "$prefix/bin" templ
        ;;
      tailwindcss)
        curl -fsSL "https://github.com/tailwindlabs/tailwindcss/releases/download/${v}/tailwindcss-${os}-${arch}" \
          -o "$prefix/bin/tailwindcss"
        chmod +x "$prefix/bin/tailwindcss"
        ;;
    esac

    # Explicit per-tool outcome: one bad download never silently poisons the run.
    if bin_present "$prefix" "$tool" "$v"; then
      echo "[toolchain] $tool installed ($v)"
    else
      echo "[toolchain] ERROR: $tool failed verification after install attempt"
      errors=$((errors + 1))
      failed_tools+=" $tool"
    fi
  done

  if [[ $dry_run == false ]]; then
    echo "[toolchain] prefix=$prefix errors=$errors failed_tools:${failed_tools:-none}"
  fi
  # rc signals *syntax/usage* problems, not individual tool download issues;
  # callers inspect the deterministic failed_tools: summary above.
  return 0
}

# Only run install when invoked directly (not sourced by tests).
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  PREFIX="${1:?usage: $0 <prefix> [--dry-run] [--force]}"
  shift
  install_toolchain "$PREFIX" "$@"
fi
