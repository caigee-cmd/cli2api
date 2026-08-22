# Deploy with Docker Compose

## 1. Prepare

Official Qoder CLI login must exist on the host:

```bash
ls ~/.qoder/.auth/user
```

Optional: a richer plaintext template. Otherwise `worker/last-plain.sample.json` is used.

## 2. Configure and start

```bash
cd deploy
cp .env.example .env
# set PROXY_API_KEY to something other than change-me
# optional: QODER_HOME, QODER_HOMES, PLAIN_TEMPLATE_PATH

docker compose up -d --build
docker compose ps
```

Empty / `change-me` keys fail fast unless you also set `ALLOW_INSECURE_API_KEY=1`.

> If you sync this repo with `rsync --delete`, remember `.env` is gitignored and may be wiped.

## 3. Network notes

Default compose creates a private `cli2api` network and publishes **only** `127.0.0.1:3010`.

Mount `~/.qoder` into the worker. Worker admin APIs and proxy `/api/*` require `PROXY_API_KEY`.

Health from the host:

```bash
curl -s http://127.0.0.1:3010/health
```

To join an existing Docker network, add a local override file (do not commit host-specific names):

```yaml
# deploy/docker-compose.override.yml
networks:
  cli2api:
    external: true
    name: your-existing-network
```

## 4. Point your gateway

```text
base_url = http://qoder-api-proxy:3010/v1   # or http://127.0.0.1:3010/v1 from the host
api_key  = <same as PROXY_API_KEY>
```

## Host-process fallback

```bash
cd worker
WORKER_HOST=127.0.0.1 WORKER_PORT=3020 \
PROXY_API_KEY=dev-key ALLOW_INSECURE_API_KEY=1 \
npm start

PROXY_API_KEY=dev-key ALLOW_INSECURE_API_KEY=1 \
QODER_WORKER_URL=http://127.0.0.1:3020 \
QODER_WORKER_API_KEY=dev-key \
go run ./cmd/server
```
