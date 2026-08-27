#!/usr/bin/env bash
# scripts/vm-tier/build-image-tar.sh — docker-archive (docker save format)
# around an ALREADY-BUILT static Go binary, no Docker daemon required.
#
# Usage: build-image-tar.sh <binary> <out.tar> [tag]
#   tag default: localhost/fleet/fleetctl:local
# Base image = scratch: single layer containing only the executable.
# Importable with: sudo ctr images import <out.tar>

set -euo pipefail
bin=$1; out=$2; tag=${3:-localhost/fleet/fleetctl:local}

[[ -x "$bin" ]] || { echo "IMAGE_TAR_FAIL reason=binary_missing:$bin"; exit 1; }

wd=$(mktemp -d)
trap 'rm -rf "$wd"' EXIT
mkdir -p "$wd/rootfs"

cp "$bin" "$wd/rootfs/fleetctl"
chmod 0755 "$wd/rootfs/fleetctl"
tar -C "$wd/rootfs" --numeric-owner --owner=0 --group=0 -cf "$wd/layer.tar" fleetctl

diff_id="sha256:$(sha256sum "$wd/layer.tar" | awk '{print $1}')"
created="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

cat > "$wd/config.json" <<EOF
{"architecture":"amd64","os":"linux","created":"$created",
 "config":{"Entrypoint":["/fleetctl"],"Cmd":["version"]},
 "rootfs":{"type":"layers","diff_ids":["$diff_id"]},
 "history":[{"created":"$created","created_by":"fleet block03 deterministic build"}]}
EOF
cat > "$wd/manifest.json" <<EOF
[{"Config":"config.json","RepoTags":["$tag"],"Layers":["layer.tar"]}]
EOF

tar -C "$wd" -cf "$out" manifest.json config.json layer.tar
echo "IMAGE_TAR_OK out=$out tag=$tag size=$(du -h "$out" | cut -f1)"
