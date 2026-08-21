# Deploy with Docker Compose

## 1. Prepare

```bash
# Qoder login state must exist on the host
ls ~/.qoder/.auth/user

# optional but recommended: captured plaintext template
ls /tmp/qoder-wasm-spike/last-plain.json
```

## 2. Configure and start

```bash
cd deploy
cp .env.example .env
# set PROXY_API_KEY

docker compose up -d --build
docker compose ps
```

> If you sync this repo with `rsync --delete`, remember `.env` is gitignored and may be wiped. Recreate it before `compose up`.

## 3. Network notes

Default compose joins an external Docker network and publishes **no host ports**.

Adjust `deploy/docker-compose.yml` if your network name / volume paths differ.

Health from another container on the same network:

```bash
wget -qO- http://qoder-api-proxy:3010/health
wget -qO- http://qoder-auth-worker:3020/health
```

## 4. Point your gateway

Create an OpenAI-compatible upstream/account with:

```text
base_url = http://qoder-api-proxy:3010/v1
api_key  = <same as PROXY_API_KEY>
```

## Host-process fallback

Worker:

```bash
cd worker
WORKER_HOST=127.0.0.1 WORKER_PORT=3020 PROXY_API_KEY=dev-key node src/daemon.mjs
```

Proxy:

```bash
QODER_WORKER_URL=http://127.0.0.1:3020 \
QODER_WORKER_API_KEY=dev-key \
PROXY_API_KEY=dev-key \
go run ./cmd/server
```
