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

| Provider | Region | Login / import | Status |
|----------|--------|----------------|--------|
| Qoder | Global / CN | Browser OAuth, PAT, `qoder-native-v1` | Stable |
| WorkBuddy | Global / CN | Browser OAuth, `workbuddy-oauth-v1` | Stable |
| Trae Work | CN | Browser OAuth, `trae-oauth-v1` | Stable |
| Devin | Global | Browser OAuth, `devin-session-v1` | Experimental |
| Command Code | Global | API key, `command-key-v1` | Experimental |
| Codex | Global | Browser OAuth, `codex-oauth-v1` | Experimental |
| Zhipu GLM | CN | API key, `zhipu-key-v1` | Experimental |
| QwenWork | Global / CN | Browser OAuth, `qwenwork-oauth-v1` | Experimental |
| Kimi Code | CN | Browser OAuth, `kimi-oauth-v1` | Experimental |
| CodeArts | CN | Browser OAuth, `codearts-oauth-v1` | Experimental |
| Kiro | US East / EU Central | Browser OAuth, API key, `kiro-oauth-v1` | Experimental |
| Muse (Meta) | Global | Browser OAuth, API key, `meta-oauth-v1` / `meta-key-v1` | Experimental |
| Doubao Work | CN | Cookie import, `doubaowork-cookie-v1` | Experimental |
| MiniMax Code | Global / CN | Browser OAuth, `minimaxcode-oauth-v1` | Experimental |

The Status column matches the console's account picker: Qoder, WorkBuddy, and
Trae are the stable logins, and everything else is an experimental adapter that
is not a production promise. The authoritative list is the descriptor registry
in `internal/providers/registry.go`.

Qoder CN, WorkBuddy, and Trae are implemented; live-account acceptance is still
pending. Kimi Code CN and CodeArts have single-account end-to-end acceptance;
CodeArts quota-exhaustion failover is still pending. A PAT-backed Qoder account
runs in-process. A Qoder account that still carries the legacy `qoder-native-v1`
credential keeps one isolated Node worker and HOME until you sign it in again
with a PAT. Every other provider uses a Go in-process adapter. All durable
credentials stay in SQLite.

Check-in is declared per provider and per region. Providers that declare it
today are WorkBuddy, QwenWork, Qoder, Trae, MiniMax Code, CodeArts, and Doubao
Work, and each offers a manual check-in on the account card. Automatic check-in
is opt-in per account and disabled by default; the default time for each
provider is set under **System → Automatic check-in**, and an account can
override it. WorkBuddy also has a per-account token keepalive, disabled by
default. Enable either only if you want those automatic account operations.

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
Global and Qoder proxies accept HTTP(S) only; every in-process provider's
account proxy also accepts SOCKS5. Use `direct` or `none` to bypass proxy
inheritance.

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
| `QODER_STREAM_TTFB_MS` | `60000` | Qoder stream first-byte deadline in ms (response head + first meaningful SSE data line after 200), Go side and worker; min `5000`; timeout fails over to the next account |
| `QODER_STREAM_TOTAL_MS` | `300000` | Qoder worker mid-stream stall timeout in ms: aborts only after this long with no new chunk (resets per chunk, not a wall-clock cap); never below the TTFB value |
| `QODER_SIGNATURE_SECRET` | built-in | Override the COSY request-signing secret for in-process Qoder; leave unset unless upstream rotates it |
| `QODER_PROXY_URL` | empty | Initial global outbound proxy: `http(s)://`, `direct`, or `none`; saved console settings take precedence |
| `QODERCLI_JS` | image default | Pinned Qoder Global CLI bundle |
| `QODERCNCLI_JS` | image default | Pinned Qoder CN CLI bundle |
| `UPDATE_GITHUB_TOKEN` | empty | Optional GitHub token for release checks |
| `UPDATE_GITHUB_REPO` | `caigee-cmd/cli2api` | Release repository (`owner/repo`) whose GitHub releases the console follows; set it to `caigee-cmd/cli2api` to follow upstream instead |
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
| `GET` | `/health` | Liveness probe; no API key required |
| `GET` | `/ready` | Readiness probe; 503 while draining or updating |
| `GET` | `/v1/models` | Model catalog |
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat |
| `POST` | `/v1/messages` | Anthropic-compatible messages |
| `POST` | `/v1/responses` | OpenAI Responses-compatible API |
| `GET/POST/PATCH/DELETE` | `/api/*` | Console management API |

Console `/api/*` requires the administrator key. `/v1/*` accepts that key or an
enabled client key. `/health`, `/ready`, static frontend resources, and `/v1/*`
CORS preflight `OPTIONS` do not require authentication. Repeated invalid API
keys from one client are rate-limited and answered with `429` and
`Retry-After`.

`/health` also reports `log_dropped` (request-log writes discarded after the
queue grace period) and `log_queue` (writes waiting behind the SQLite writer).

Messages / Responses are stateless compatibility APIs, not full official API
implementations. They do not store server-side conversations or execute
upstream-specific tools. Image input follows the upstream model catalog:
WorkBuddy's `supportsImages` models (DeepSeek V4 / V4.1, GLM, Kimi, and
others), Qoder's vision models, QwenWork `is_vl` models, Kimi Code models that
declare image input, experimental Devin base64 data URLs, and Codex image
modalities all pass through, and each model's declared capability is exposed as
`supports_images` on `/v1/models` and `/api/models`. Trae, Command, and Zhipu do
not declare image input.

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

Installers take the updater binary from the running container first, so the
installed updater always matches the deployed image and no release asset has to
be downloaded. They fall back to checksum-verified release binaries or a local
Go build when the container is unreachable. Both installers default to
`ghcr.io/caigee-cmd/cli2api` and `caigee-cmd/cli2api`; override them with
`CLI2API_IMAGE_REPOSITORY` and `CLI2API_GITHUB_REPOSITORY` when the deployment
follows another repository.

On Linux, `./scripts/start.sh` runs the installer automatically when no
`cli2api-updater.service` exists yet (set `CLI2API_SKIP_UPDATER_INSTALL=1` to
skip it). The console's **Download update** then pulls the target image on the
host, recreates the container, health-checks the new version, and rolls back to
the previous image and SQLite backup if that check fails. The updater binary is
copied out of the pulled image, so an update never depends on GitHub asset
downloads.

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


## Cache affinity and message rectification

These switches follow the reference 2api gateways and are off by default except
the Anthropic thinking rectifier, which is on because OpenAI-compatible
upstreams do not validate Anthropic thinking signatures.

| Variable | Default | Description |
|---|---|---|
| `CLI2API_MSGSTAT_SHARDS` | `0` | When a request has no conversation id and no content seed, bound it to N account buckets per (API key, model, system prefix, tools). A chat that already has a user message keeps its content-seed affinity. Mirrors `WB_MSGSTAT_SHARDS`; `0` disables the fallback. |
| `CLI2API_AFFINITY_DEBUG` | `0` | Log the session-affinity source at prepare time, and prompt/cache-token diagnostics when the request finishes, on `/v1/chat/completions`, `/v1/messages`, and `/v1/responses`. |
| `CLI2API_THINKING_RECTIFIER` | `on` | Set `off` to replay historical Anthropic thinking blocks unchanged. |
| `X-CLI2API-Session` / `X-Conversation-Id` / `X-Session-Id` / `X-Thread-Id` | — | Explicit conversation id. Body `conversation_id` / `conversationId` / `session_id` / `metadata.*` aliases are also recognised so client turns stay on one account and keep the upstream prompt cache warm. |
