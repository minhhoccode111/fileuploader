#!/usr/bin/env bash
# logs.sh — follow container logs (default: app; args pass through).
# Usage:   ./scripts/logs.sh [redis|db|minio] [--since 10m ...]
set -Eeuo pipefail
cd "$(dirname "$0")/.."

exec docker compose logs -f --tail=100 "${@:-app}"
