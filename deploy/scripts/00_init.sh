#!/usr/bin/env bash
# 00_init.sh — bootstrap the fileuploader stack on the VPS.
#
# Idempotent: re-run any time. Validates .env and the compose file,
# starts the stack, then waits for the app to answer /healthz.
#
# Usage:   ./scripts/00_init.sh          (from ~/fileuploader)
# Fails:   no .env, docker/compose missing, invalid compose config,
#          stack did not come up, /healthz never answered.
set -Eeuo pipefail
cd "$(dirname "$0")/.."

say()  { printf '\n==> %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

[[ -f .env ]] || die "no .env — cp .env.example .env, then set every CHANGE_ME"
command -v docker >/dev/null || die "docker is not installed"
docker compose version >/dev/null 2>&1 || die "docker compose v2 is missing"

say "Validating compose config"
docker compose config -q

say "Starting stack"
docker compose up -d || die "up failed — if the app image is missing on \
Docker Hub, merge to main once so CI pushes it, then re-run this script"

say "Waiting for /healthz"
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:8090/healthz >/dev/null 2>&1; then
    say "Healthy — http://127.0.0.1:8090/healthz"
    docker compose ps
    exit 0
  fi

  sleep 2
done

die "app never answered /healthz — check: docker compose logs app"
