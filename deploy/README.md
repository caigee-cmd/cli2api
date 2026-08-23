# Deploy with Docker Compose

## 1. Configure

```bash
cd deploy
cp ../.env.example .env
# set a real PROXY_API_KEY
```

The deployment is one container. It contains the Go control plane, Node runtime,
pinned qodercli, and frontend. SQLite and per-account runtime homes persist in the
`qoder-data` volume.

## 2. Start

```bash
docker compose pull
docker compose up -d
docker compose ps
curl -s http://127.0.0.1:3010/health
```

To build from the checked-out source instead, run `docker compose up -d --build`.

Only `127.0.0.1:3010` is published. Account and credential operations require
`PROXY_API_KEY`.

## 3. Existing Qoder login

By default Compose mounts `${QODER_HOME:-$HOME/.qoder}` read-only at
`/import/.qoder`. If SQLite is empty and `.auth/user` plus `.auth/machine_id` exist,
the service imports them once as the first enabled account.

After startup, add further accounts from `/accounts` using browser OAuth, PAT, or a
`qoder-native-v1` credential bundle.

## 4. Gateway

For another container on the same Docker network:

```text
base_url = http://qoder-api-proxy:3010/v1
api_key  = <same as PROXY_API_KEY>
```
