#!/usr/bin/env bash
# Copy node/ to each Spark node over SSH and (re)start the exporters there.
#
# Usage:
#   scripts/deploy-exporters.sh user@spark-node-1 user@spark-node-2
#
# Requires: ssh + rsync access to the nodes (key-based auth recommended), and
# Docker + the NVIDIA Container Toolkit already installed on each node.

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <ssh-target> [<ssh-target> ...]" >&2
  echo "example: $0 me@192.168.1.101 me@192.168.1.102" >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
remote_dir="sparkmon-exporters"

for target in "$@"; do
  echo "==> [$target] syncing exporter stack"
  ssh "$target" "mkdir -p ~/$remote_dir"
  rsync -az --delete "$repo_root/node/" "$target:~/$remote_dir/"

  echo "==> [$target] pulling images + starting"
  ssh "$target" "cd ~/$remote_dir && docker compose pull --quiet && docker compose up -d"

  echo "==> [$target] status"
  ssh "$target" "cd ~/$remote_dir && docker compose ps"
  echo
done

echo "Done. Point prometheus/prometheus.yml at <host>:9100 and <host>:9400 for each node."
