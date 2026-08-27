# Deploy with Docker Compose

## 1. Start

The deployment is one container containing the Go control plane, Node runtime,
pinned qodercli, and frontend. SQLite and account credentials persist in the
`qoder-data` volume; per-account Qoder runtime homes use tmpfs.

From the repository root, use:

macOS / Linux:

```bash
./scripts/start.sh
```

Windows PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start.ps1
```

Both launchers create `deploy/.env` if needed, pull the published image, fall
back to a local build when necessary, and wait for `/health`.

To run Compose directly:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
```

Add `--build` to build from the checked-out source. Only `127.0.0.1:3010` is
published. Account and credential operations require the key stored in SQLite.

## 2. Qoder login storage

After startup, add accounts from `/accounts` using browser OAuth, PAT, or a
`qoder-native-v1` credential bundle. The encrypted Qoder user blob and matching
`machine_id` are stored in SQLite. Workers materialize those files only under the
per-account tmpfs runtime directory while running.

## 3. Gateway

For another container on the same Docker network:

```text
base_url = http://qoder-api-proxy:3010/v1
api_key  = <same key printed on first startup>
```

## 4. Connect an OpenAI client

```bash
export CLI2API_API_KEY='paste-the-key-printed-on-first-start'

curl http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $CLI2API_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "messages": [{"role": "user", "content": "Reply with OK only"}],
    "stream": false
  }'
```

PowerShell equivalent:

```powershell
$env:CLI2API_API_KEY = "paste-the-key-printed-on-first-start"
$Headers = @{ Authorization = "Bearer $env:CLI2API_API_KEY" }
$Body = @{
  model = "qwen3.7-plus"
  messages = @(@{ role = "user"; content = "Reply with OK only" })
  stream = $false
} | ConvertTo-Json -Depth 4
Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:3010/v1/chat/completions" -Headers $Headers -ContentType "application/json" -Body $Body
```

Pin a request to a specific account with the `X-Qoder-Account: acc_...` header.

## 5. Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `QODER_DATA_DIR` | `/data` | SQLite database and durable account credentials |
| `QODER_RUNTIME_DIR` | `/run/cli2api` | Ephemeral per-account Qoder runtime homes |
| `QODER_MAX_INFLIGHT` | `4` | Maximum concurrent requests per account |
| `QODER_WORKER_BASE_PORT` | `32100` | Internal worker port range |
| `QODERCLI_JS` | image default | Pinned Qoder Global CLI bundle |
| `QODERCNCLI_JS` | image default | Pinned Qoder CN CLI bundle |
| `UPDATE_GITHUB_TOKEN` | empty | Optional GitHub token for release checks |
| `UPDATE_AGENT_URL` | empty | Docker Desktop host updater URL, written by the installer |
| `UPDATE_AGENT_TOKEN` | empty | Docker Desktop updater token, written by the installer |
| `CLI2API_UPDATER_SOCKET_DIR` | platform-specific | Host directory mounted read-only for the Linux updater socket |

The API key is generated once and stored in SQLite. There is no environment-variable bootstrap path; changing container environment variables does not replace the stored key.

## 6. Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | Health probe; no API key required |
| `GET` | `/v1/models` | Model catalog |
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat |
| `GET/POST` | `/api/*` | Console management API |

All console and API routes except `/health` require the API key stored in SQLite.

## 7. Managed next-version update

Start `qoder-api-proxy` once before installing the optional host updater.

| Host | Container platform | Updater asset |
|------|--------------------|---------------|
| Linux x86-64 | `linux/amd64` | `cli2api-updater_linux_amd64` |
| Linux ARM64 | `linux/arm64` | `cli2api-updater_linux_arm64` |
| macOS Intel | Docker Desktop `linux/amd64` | `cli2api-updater_darwin_amd64` |
| macOS Apple Silicon | Docker Desktop `linux/arm64` | `cli2api-updater_darwin_arm64` |
| Windows x86-64 | Docker Desktop Linux containers | `cli2api-updater_windows_amd64.exe` |
| Windows ARM64 | Docker Desktop Linux containers | `cli2api-updater_windows_arm64.exe` |

Release assets include a SHA256 manifest. Installers use a verified prebuilt
updater whenever possible. Linux first copies the matching binary from the
running container; older releases without assets can use the latest compatible
asset or fall back to a local Go `1.25.6+` build.

macOS + Docker Desktop:

```bash
./deploy/install-updater.sh
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d --force-recreate qoder-api-proxy
```

Linux + systemd:

```bash
sudo ./deploy/install-updater.sh
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d --force-recreate qoder-api-proxy
```

Windows + Docker Desktop in Linux-container mode, from the logged-in Docker user's PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\install-updater.ps1
docker compose --env-file deploy\.env -f deploy\docker-compose.yml up -d --force-recreate qoder-api-proxy
```

The application container never receives the Docker socket. Linux uses a private
Unix Socket. macOS runs a per-user LaunchAgent and Windows runs a current-user
Scheduled Task; both Docker Desktop platforms use an authenticated updater bound
to `127.0.0.1` and reached through `host.docker.internal`.

Before replacement, the Go process pauses new API requests, waits for active
requests to drain, and creates a verified SQLite snapshot in `/data/backups`.
The updater recreates only `qoder-api-proxy`; it never runs
`docker compose down -v`, and it verifies that the same `/data` mount remains
attached. If the new version fails its versioned health check, the updater
restores both the previous image and the pre-update SQLite snapshot, then pins
`CLI2API_IMAGE` to the previous version for future restarts. The five most recent
snapshots are retained.

The updater remains unavailable for development builds without a semantic
version. The System page only offers the immediate next stable release.

The updater API currently reports protocol version `1`. Protocol `0` remains
temporarily accepted for bootstrap compatibility; unknown versions fail closed.

## 8. Platform notes

- `scripts/start.sh` and `deploy/install-updater.sh` are for macOS/Linux.
- `scripts/start.ps1` and `deploy/install-updater.ps1` are the Windows equivalents.
- Published application images are Linux-only and multi-architecture; macOS and Windows run them through Docker Desktop.
- Published host updater assets cover Linux, macOS, and Windows on `amd64` and `arm64`.
- Run the Windows updater installer as the same signed-in user that runs Docker Desktop; do not install it as `LocalSystem`.
- Keep `deploy/.env` private because Docker Desktop mode stores the updater token there.
- Do not remove `qoder-data` unless you intentionally want to delete SQLite accounts and credentials.
