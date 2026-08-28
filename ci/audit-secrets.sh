#!/usr/bin/env bash
# ci/audit-secrets.sh — WO-19 secrets audit. Scans the WORKING TREE and
# GIT HISTORY for secret-shaped material: known secret KEY=value
# assignments with non-placeholder values, and private key blocks.
# Test fixtures use dummy values by design and are excluded.
# Exit 0 clean, 1 findings, 2 tooling error.
set -u
repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo" || exit 2
# only VALUE-SHAPED matches are findings: key= + >=20 token-ish chars.
# Code references (prefix matching, tests writing "x"/"dummy") never match.
KEYVAL='(CF_API_TOKEN|TUNNEL_TOKEN|OPENCHAMBER_UI_PASSWORD|ADMIN_SECRET|SECRET_KEY|AIOSTREAMS_AUTH|ADDON_SHARED_SECRET|ADDON_STREAM_TOKEN)=["\x27]?[A-Za-z0-9+/_-]{20,}["\x27]?'
findings=0

# 1. working tree
while IFS= read -r f; do
  m="$(grep -n -E "$KEYVAL" "$f" 2>/dev/null)"
  if [ -n "$m" ]; then
    echo "FINDING [tree] $f:"
    echo "$m"
    findings=1
  fi
  if grep -q "BEGIN .*PRIVATE KEY" "$f" 2>/dev/null; then
    echo "FINDING [tree] $f: private key block"
    findings=1
  fi
done < <(find . -type f \
  ! -path "./.git/*" ! -path "./.toolchain/*" ! -path "./.vm/*" \
  ! -path "./dist/*" ! -path "./internal/fleet/testdata/*" \
  ! -path "./tests/*" ! -path "./ci/audit-secrets.sh" \
  ! -name "*.md" -print)

# 2. git history: any commit touching those key names outside fixtures
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  hits="$(git log --all --oneline -G"$KEYVAL" 2>/dev/null | head -20)"
  if [ -n "$hits" ]; then
    echo "FINDING [history] known secret key names appear in history (review + rotate if real):"
    echo "$hits"
    findings=1
  fi
fi

if [ "$findings" -eq 0 ]; then
  echo "SECRETS AUDIT OK tree+history clean"
  exit 0
fi
echo "SECRETS AUDIT FAIL — rotate any real values immediately"
exit 1
