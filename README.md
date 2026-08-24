# CLI2API

[中文](README_ZH.md) · [Issues](https://github.com/caigee-cmd/cli2api/issues) · [Contributing](CONTRIBUTING.md)

[![CI](https://github.com/caigee-cmd/cli2api/actions/workflows/ci.yml/badge.svg)](https://github.com/caigee-cmd/cli2api/actions/workflows/ci.yml)
[![Docker Image](https://ghcr-badge.egpl.dev/caigee-cmd/cli2api/latest_tag?label=docker&color=blue)](https://github.com/caigee-cmd/cli2api/pkgs/container/cli2api)
[![License](https://img.shields.io/github/license/caigee-cmd/cli2api)](LICENSE)

Turn **your own Qoder CLI login** into a local OpenAI-compatible API.

CLI2API is an unofficial, self-hosted gateway for Qoder. Keep using OpenAI SDKs, Codex, CherryStudio, and other compatible clients—just point their Base URL at this local service.

![CLI2API console](docs/assets/console.png)

> [!IMPORTANT]
> CLI2API is not affiliated with or endorsed by Qoder. Use only accounts you are authorized to use, and follow the terms of Qoder and related services.

## What you get

- A local OpenAI-compatible `POST /v1/chat/completions` endpoint
- Streaming and non-streaming responses, tool calls, and `reasoning_content`
- A web console for Qoder accounts, models, and API testing
- Multi-account routing, account pinning, concurrency limits, cooldowns, and failover
- A Docker Compose deployment for personal development, homelabs, and private servers

CLI2API does not spawn a full Qoder CLI Agent for every request. Authentication, WASM encoding, and the Qoder cloud HTTP/SSE connection stay warm in long-lived workers.

## Quick start

Requirements: Docker Desktop on macOS/Windows, or Docker Engine with Compose on Linux, plus a Qoder account you control. On Windows, Docker Desktop must be using Linux containers.

macOS / Linux:

```bash
git clone https://github.com/caigee-cmd/cli2api.git
cd cli2api
./scripts/start.sh
```

Windows PowerShell:

```powershell
git clone https://github.com/caigee-cmd/cli2api.git
Set-Location cli2api
powershell -ExecutionPolicy Bypass -File .\scripts\start.ps1
```

The launcher creates `deploy/.env` when needed, starts the published image, and builds locally if the image is unavailable.

On first startup, the service generates a random API key, stores it in SQLite, and prints it once in the logs. Save it first:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yml logs qoder-api-proxy
```

Open `http://127.0.0.1:3010`, sign in with that key, and add Qoder accounts from **Accounts**.

The default deployment publishes only `127.0.0.1:3010`; it does not expose the service publicly.

## Managed updates

Start CLI2API once, then install the optional host updater.

| Host | Application runtime | Host updater |
|------|---------------------|--------------|
| Linux `amd64` / `arm64` | matching Linux Docker image | systemd + Unix Socket |
| macOS Intel / Apple Silicon | Docker Desktop Linux container | per-user LaunchAgent |
| Windows `amd64` / `arm64` | Docker Desktop Linux container | current-user Scheduled Task |

The main API service remains a Linux container; native macOS and Windows application binaries are not published. Release images support `linux/amd64` and `linux/arm64`. Each release also includes six native updater assets and `cli2api-updater_checksums.txt`.

Installers prefer a checksum-verified prebuilt updater. Linux first reuses the architecture-matched updater inside the running container. If an older release has no updater asset, the installers try the latest protocol-compatible asset and finally fall back to a local Go `1.25.6+` build.

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

Windows + Docker Desktop, from PowerShell as the logged-in Docker user:

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\install-updater.ps1
docker compose --env-file deploy\.env -f deploy\docker-compose.yml up -d --force-recreate qoder-api-proxy
```

Linux uses a private Unix Socket. macOS uses a per-user LaunchAgent, and Windows uses a current-user Scheduled Task; both Docker Desktop platforms connect through a token-protected updater bound to `127.0.0.1`. The Windows installer also verifies that a container can reach the updater through `host.docker.internal` when `qoder-api-proxy` already exists.

The **System** page permits only the immediate next stable release. It does not accept custom targets, skip releases, or install prereleases. Before replacement, the service pauses new requests, drains active requests, and creates an integrity-checked SQLite snapshot under `/data/backups`.

Only `qoder-api-proxy` is recreated; the `qoder-data` volume is never deleted. A failed versioned health check restores both the previous image and the pre-update SQLite snapshot, and pins `CLI2API_IMAGE` back to the previous version for future restarts. The five most recent snapshots are retained.

## Maintainer release

After `main` passes CI, publish the next patch release with one command:

```bash
gh workflow run release.yml --ref main
```

Write bilingual user-facing notes in `CHANGELOG.md` under `## Unreleased` before publishing. Each change needs a matching bullet in `### English` and `### 中文`; the workflow copies those notes into the GitHub Release and the console update page.

The workflow waits for the exact `main` commit to pass CI, calculates the next patch from the latest published stable release, creates an invisible draft release, builds six checksum-verified updater binaries, verifies the `linux/amd64` and `linux/arm64` image manifest, and only then publishes the GitHub Release and moves the stable image aliases. After publication it freezes the Unreleased notes under the new version heading. Do not create or push the version tag manually.

You can also use **Actions → Release → Run workflow**. If a pre-publication job fails, use **Re-run failed jobs** on the same run; the draft release remains invisible to application update checks.

## Connect an OpenAI client

Configure your client with:

```text
Base URL: http://127.0.0.1:3010/v1
API Key:  <the generated key printed on first startup>
```

Or make a direct request:

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

Without an account header, the scheduler selects a ready account. Pin a request to a specific account with:

```text
X-Qoder-Account: acc_...
```

## Use cases

- Connect Qoder to local or private-server tooling
- Reuse OpenAI-compatible clients and scripts
- Route requests across multiple Qoder accounts with failover
- Keep Qoder login state available without starting a full CLI Agent per request

Qoder is the only supported upstream at this time. CLI2API is a local gateway; it does not provide accounts, quotas, or an official Qoder API service.

## How it works

```text
OpenAI client
  -> Go API + SQLite account control plane
    -> one isolated Node worker per Qoder account
      -> Qoder cloud HTTP/SSE API
```

Each enabled account gets its own Node process and runtime directory so Qoder WASM state is not shared across accounts. Go owns persistence, scheduling, concurrency limits, cooldowns, failover, and child-process lifecycle.

## Features

- Browser Device Flow OAuth, PAT, and `qoder-native-v1` credential import/export
- OpenAI-compatible `GET /v1/models`
- Streaming and non-streaming responses
- Tool calls and `reasoning_content`
- Multiple Qoder accounts with pinning, routing, cooldown, and failover
- React + Tailwind + HeroUI console with light and dark themes
- Persistent SQLite credentials with ephemeral per-account runtime homes
- GitHub Actions for tests, container builds, and GHCR releases

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `QODER_DATA_DIR` | `/data` | SQLite database and durable account credentials |
| `QODER_RUNTIME_DIR` | `/run/cli2api` | Ephemeral per-account Qoder runtime homes |
| `QODER_MAX_INFLIGHT` | `4` | Maximum concurrent requests per account |
| `QODER_WORKER_BASE_PORT` | `32100` | Internal worker port range |
| `UPDATE_GITHUB_TOKEN` | empty | Optional GitHub token for release checks |
| `UPDATE_AGENT_URL` | empty | Docker Desktop host updater URL, written by the installer |
| `UPDATE_AGENT_TOKEN` | empty | Docker Desktop updater token, written by the installer |
| `CLI2API_UPDATER_SOCKET_DIR` | platform-specific | Host directory mounted read-only for the Linux updater socket |

The API key is generated once and stored in SQLite. There is no environment-variable bootstrap path; changing container environment variables does not replace the stored key.

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | Health probe; no API key required |
| `GET` | `/v1/models` | Model catalog |
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat |
| `GET/POST` | `/api/*` | Console management API |

All console and API routes except `/health` require the API key stored in SQLite.

## Development

Requirements: Go `1.25.6+`, Node.js `20+`, npm, and Docker for container development.

```bash
# Go API
go test ./...
go vet ./...

# Qoder worker
cd worker
npm test

# Console
cd ../frontend
npm ci
npm run build
npm run lint
```

After frontend changes, run `npm run sync` to update the static assets embedded by Go:

```bash
cd frontend
npm run sync
```

Build and start the container from source:

```bash
cd deploy
docker compose up -d --build
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for repository rules and validation details.

## Documentation

- [Architecture, login, routing, and console design](docs/DESIGN.md)
- [Current milestone and development plan](docs/PLAN.md)
- [Docker Compose deployment](deploy/README.md)
- [Changelog](CHANGELOG.md)
- [Security policy](SECURITY.md)

## Security and privacy

- Never expose the service without the generated API key.
- Never commit `.qoder`, tokens, cookies, auth blobs, raw captures, or host details.
- Credential export is an explicit sensitive operation; protect exported files.
- Upstream API or Qoder CLI changes may break compatibility; qodercli is pinned and checked.
- Please report security issues privately according to [SECURITY.md](SECURITY.md), not in a public issue.

## Contributing

Issues, documentation improvements, and pull requests are welcome. Before submitting a change:

- Keep the scope clear and avoid new component libraries or unnecessary service dependencies.
- Run tests and build commands relevant to your changes.
- Remove tokens, login state, raw protocol captures, and real deployment details from the diff.

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
