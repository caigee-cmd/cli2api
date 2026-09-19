# Deploy with Docker Compose

## 1. Start

Docker Compose is the supported installation. Use Docker Engine + Compose on
Linux or Docker Desktop on macOS / Windows (Linux containers).
The `qoder-data` volume stores SQLite and account credentials. Source runs are
for development and do not support managed updates.

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

To run Compose directly, create `deploy/.env` first (an empty file is enough
for defaults; do not overwrite an existing file):

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
```

Add `--build` to build from the checked-out source. Only `127.0.0.1:3010` is
published.

Save the administrator key printed once in the first-start logs:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yml logs qoder-api-proxy
```

Open `http://127.0.0.1:3010`, sign in with that key, and add an account.
Create a separate client key in **API keys** for applications; it only grants
access to `/v1/*` and can be restricted by provider and region.

## 2. Add accounts

Use **Accounts** to add an account with its supported login method:

| Provider | Region | Login / import |
|----------|--------|----------------|
| Qoder | Global / CN | Browser OAuth, PAT, `qoder-native-v1` |
| WorkBuddy | Global / CN | Browser OAuth, `workbuddy-oauth-v1` |
| Trae Work | CN | Browser OAuth, `trae-oauth-v1` |
| Devin (experimental) | Global | Browser OAuth, `devin-session-v1` |

Qoder CN, WorkBuddy, and Trae still need live-account acceptance; Devin is not
claimed production-ready. Qoder uses one isolated Node worker per account.
Other providers use Go in-process adapters. All durable credentials stay in SQLite.

WorkBuddy daily check-in and token keepalive are per-account opt-in and disabled
by default. Enable them only if you want those automatic account operations.

## 3. Connect a client

Use `http://127.0.0.1:3010/v1` on the host. For another container on the same
Docker network:

```text
base_url = http://qoder-api-proxy:3010/v1
api_key  = <client key created in the console>
```

Get a model ID from **Access** or `GET /v1/models`, then test a request:

```bash
export CLI2API_API_KEY='paste-your-client-key'

curl http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $CLI2API_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "<model-id-from-v1-models>",
    "messages": [{"role": "user", "content": "Reply with OK only"}],
    "stream": false
  }'
```

PowerShell equivalent:

```powershell
$env:CLI2API_API_KEY = "paste-your-client-key"
$Headers = @{ Authorization = "Bearer $env:CLI2API_API_KEY" }
$Body = @{
  model = "<model-id-from-v1-models>"
  messages = @(@{ role = "user"; content = "Reply with OK only" })
  stream = $false
} | ConvertTo-Json -Depth 4
Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:3010/v1/chat/completions" -Headers $Headers -ContentType "application/json" -Body $Body
```

Pin a request to a specific account with the `X-Qoder-Account: acc_...` header
(a historical header name that applies to every provider). Consecutive turns of
the same conversation prefer the same account from the first user message
(including image-only turns);
`X-CLI2API-Session` remains an optional override.
Account pinning is a routing preference, not an authorization boundary: a missing
account can fall back to the pool, and cooling pins may escape within the same
provider and region. Use client-key grants to enforce access limits.

Cross-provider routing for shared model IDs is enabled by default. Use a
provider-prefixed ID such as `qoder/<model-id>` to restrict the provider.
When cross-provider routing is disabled, a provider prefix is required.
Key grants, region constraints, model support, and account readiness still apply.

## 4. Configuration

Most settings are in the console: **System** for the global proxy, **Accounts**
for per-account proxy and concurrency, and **API keys** for client access.
Global and Qoder proxies accept HTTP(S) only; WorkBuddy / Trae / Devin account
proxies also accept SOCKS5. Use `direct` or `none` to bypass proxy inheritance.

The administrator key is generated once and stored in SQLite. Rotate it in the
console; environment variables cannot replace it.

<details>
<summary>Advanced environment variables and diagnostics</summary>

| Variable | Default | Purpose |
|----------|---------|---------|
| `QODER_DATA_DIR` | `/data` | SQLite database and durable account credentials |
| `QODER_RUNTIME_DIR` | `/run/cli2api` | Ephemeral per-account runtime homes for providers that use child processes |
| `QODER_MAX_RETRY_ACCOUNTS` | `4` | Maximum accounts attempted for one request (1-64) |
| `QODER_SSE_DIAGNOSTIC_MODELS` | empty | Comma-separated Qoder model IDs for redacted SSE diagnostics in Runtime Logs; `*` enables all |
| `QODER_WORKER_BASE_PORT` | `32100` | Internal child-runtime port range |
| `QODER_PROXY_URL` | empty | Initial global outbound proxy: `http(s)://`, `direct`, or `none`; saved console settings take precedence |
| `QODERCLI_JS` | image default | Pinned Qoder Global CLI bundle |
| `QODERCNCLI_JS` | image default | Pinned Qoder CN CLI bundle |
| `UPDATE_GITHUB_TOKEN` | empty | Optional GitHub token for release checks |
| `UPDATE_AGENT_URL` | empty | Docker Desktop host updater URL, written by the installer |
| `UPDATE_AGENT_TOKEN` | empty | Docker Desktop updater token, written by the installer |
| `CLI2API_UPDATER_SOCKET_DIR` | platform-specific | Host directory mounted read-only for the Linux updater socket |

These are process settings; Compose passes only variables declared in its
`environment` section. For variables not listed there, add them through a local
`deploy/docker-compose.override.yml` and include that file with `-f` when running
Compose directly. An entry in `deploy/.env` alone is not enough.

For Qoder stream diagnostics, set `QODER_SSE_DIAGNOSTIC_MODELS` to a model ID
from `/v1/models` and recreate the container. Diagnostics record event metadata,
not prompts, responses, tool arguments, or credentials.

</details>

## 5. Endpoints and limits

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | Health probe; no API key required |
| `GET` | `/v1/models` | Model catalog |
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat |
| `POST` | `/v1/messages` | Anthropic-compatible messages |
| `POST` | `/v1/responses` | OpenAI Responses-compatible API |
| `GET/POST/PATCH/DELETE` | `/api/*` | Console management API |

Console `/api/*` requires the administrator key. `/v1/*` accepts that key or an
enabled client key. `/health`, static frontend resources, and `/v1/*` CORS
preflight `OPTIONS` do not require authentication.

Messages / Responses are stateless compatibility APIs, not full official API
implementations. They do not store server-side conversations or execute
upstream-specific tools. Qoder supports images where the model allows them;
experimental Devin accepts base64 data-URL images. WorkBuddy / Trae do not
support images. File inputs are rejected.

## 6. Managed updates (optional)

Start `qoder-api-proxy` once before installing the optional host updater.

| Host | Container platform | Updater asset |
|------|--------------------|---------------|
| Linux x86-64 | `linux/amd64` | `cli2api-updater_linux_amd64` |
| Linux ARM64 | `linux/arm64` | `cli2api-updater_linux_arm64` |
| macOS Intel | Docker Desktop `linux/amd64` | `cli2api-updater_darwin_amd64` |
| macOS Apple Silicon | Docker Desktop `linux/arm64` | `cli2api-updater_darwin_arm64` |
| Windows x86-64 | Docker Desktop Linux containers | `cli2api-updater_windows_amd64.exe` |
| Windows ARM64 | Docker Desktop Linux containers | `cli2api-updater_windows_arm64.exe` |

Installers prefer checksum-verified release binaries and fall back to the
running container's binary or a local Go build when needed.

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

In **System**, download the latest stable update, then confirm installation.
Confirmation pauses new API traffic, creates a SQLite snapshot, and replaces
the container. Active connections may be interrupted; clients should retry.
Development builds without a semantic version cannot use managed updates.

Updates jump directly to the latest stable release. The console shows skipped
versions and offers rollback to one of the three previous stable releases.
If an upgrade fails its health check or updater replacement, the updater restores
the previous image and pre-update database snapshot. The five most recent
snapshots are retained in `/data/backups`.

The flow is implemented, but live upgrade / rollback acceptance remains pending.
Keep a separate database backup before upgrading.

Keep `deploy/.env` private: Docker Desktop mode stores an updater token there.
Do not remove `qoder-data` or run `docker compose down -v` unless you intend to
delete the accounts and credentials.
