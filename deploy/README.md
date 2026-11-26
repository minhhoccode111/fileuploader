# deploy/ — production stack for the VPS

Lives on the VPS at `~/fileuploader` (rsynced from this directory). Caddy on
the host terminates TLS and proxies `fileuploader.minhhoccode111.com` to
`127.0.0.1:8090`; the snippet is `snippets/fileuploader.minhhoccode111.com`
in the devops/caddy repo.

```
~/fileuploader/
├── docker-compose.prod.yml   # db, redis, minio, app — no nginx
├── .env                      # gitignored, never in this repo
├── .env.example              # template for it
└── scripts/
    ├── 00_init.sh            # bootstrap: validate, up -d, wait for /healthz
    ├── ci-deploy.sh          # forced command of the CI deploy key
    └── logs.sh               # follow logs
```

Only `app` is reachable from the host: `127.0.0.1:8090` (Caddy) and
`127.0.0.1:9090` (metrics/pprof, loopback only). db, redis and minio publish
no ports at all.

## First deploy

1. Rsync this directory to the VPS:
   `rsync -avP deploy/ mhc:~/fileuploader/`
2. On the VPS: `cd ~/fileuploader && cp .env.example .env && vi .env`
   — set every `CHANGE_ME`; the password in `PG_URL` must equal
   `POSTGRES_PASSWORD`, and `MINIO_ROOT_PASSWORD` must equal
   `MINIO_SECRET_KEY`.
3. Add the Caddy snippet (devops/caddy): `./sync.sh` + `./scripts/deploy.sh`.
4. Push the app image once — merge `develop` into `main` so CI runs, or build
   and push by hand.
5. On the VPS: `./scripts/00_init.sh`.

After that, every push to `main` on GitHub deploys automatically:
CI runs `go test -race`, builds the image, pushes it to Docker Hub as
`latest` + commit sha, then sshes in and runs `scripts/ci-deploy.sh`.

## GitHub secrets

| Secret                | Value                                     |
| --------------------- | ----------------------------------------- |
| `DOCKERHUB_USERNAME`  | `minhhoccode111`                          |
| `DOCKERHUB_TOKEN`     | Docker Hub access token (write scope)     |
| `VPS_HOST`            | `ssh.minhhoccode111.com`                  |
| `VPS_PORT`            | `2222`                                    |
| `VPS_USER`            | `mhc`                                     |
| `VPS_SSH_PRIVATE_KEY` | the `fileuploader-ci` ed25519 private key |
| `VPS_KNOWN_HOSTS`     | `ssh-keyscan -p 2222 <host>` output       |

The CI key is pinned in `authorized_keys` with
`restrict,command=".../scripts/ci-deploy.sh"` — it can deploy and do nothing
else.

## Rollback

`ci-deploy.sh` honors `IMAGE_TAG` from `.env`:

```
echo 'IMAGE_TAG=<commit-sha-from-Docker-Hub>' >> .env   # freeze
docker compose up -d app                                 # apply
```

Remove the line (or set `latest`) to resume following CI.

## Day-to-day

```bash
./scripts/logs.sh                 # follow app logs
./scripts/logs.sh db --since 10m  # db, last 10 minutes
./scripts/00_init.sh              # re-run after editing compose or .env
docker compose ps                  # container state
```

Migrations need no step of their own: the image builds with the `migrate`
tag and the app applies them at startup.
