#!/usr/bin/env bash
# ci-deploy.sh — pull the app image and restart the container.
#
# Forced command of the fileuploader-ci deploy key (authorized_keys):
# the key can run this script and nothing else. Invoked by GitHub
# Actions over ssh; never run by hand (use 00_init.sh instead).
set -Eeuo pipefail

cd "$(dirname "$0")/.."

tag="$(grep -E '^IMAGE_TAG=' .env | cut -d= -f2- || true)"
: "${tag:=latest}"

printf '\n==> Deploying image tag: %s\n' "$tag"

docker compose pull app
docker compose up -d app
docker image prune -f

docker compose ps app
