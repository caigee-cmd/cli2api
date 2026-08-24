# Deploy with Docker Compose

## 1. Configure

```bash
cd ..
./scripts/start.sh
```

The launcher creates `deploy/.env` if needed. The first startup generates a
random key, stores it in SQLite, and prints it once.
The deployment is one container containing the Go control plane, Node runtime,
pinned qodercli, and frontend. SQLite and account credentials persist in the
`qoder-data` volume; per-account Qoder runtime homes use tmpfs.

## 2. Start

```bash
cd deploy
docker compose up -d
```

To build from the checked-out source instead, run `docker compose up -d --build`.

Only `127.0.0.1:3010` is published. Account and credential operations require the
key stored in SQLite.

## 3. Qoder login storage

After startup, add accounts from `/accounts` using browser OAuth, PAT, or a
`qoder-native-v1` credential bundle. The encrypted Qoder user blob and matching
`machine_id` are stored in SQLite. Workers materialize those files only under the
per-account tmpfs runtime directory while running.

## 4. Gateway

For another container on the same Docker network:

```text
base_url = http://qoder-api-proxy:3010/v1
api_key  = <same key printed on first startup>
```
