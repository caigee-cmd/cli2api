# CLI2API

[中文](README_ZH.md) · [Issues](https://github.com/caigee-cmd/cli2api/issues) · [Contributing](CONTRIBUTING.md)

[![CI](https://github.com/caigee-cmd/cli2api/actions/workflows/ci.yml/badge.svg)](https://github.com/caigee-cmd/cli2api/actions/workflows/ci.yml)
[![Docker Image](https://ghcr-badge.egpl.dev/caigee-cmd/cli2api/latest_tag?label=docker&color=blue)](https://github.com/caigee-cmd/cli2api/pkgs/container/cli2api)
[![License](https://img.shields.io/github/license/caigee-cmd/cli2api)](LICENSE)

An unofficial, self-hosted OpenAI-compatible API gateway that reuses **your own Qoder CLI login** and lets OpenAI-compatible clients connect to Qoder.

CLI2API keeps authentication, WASM encoding, and the Qoder cloud HTTP/SSE connection warm in long-lived workers instead of spawning a full Qoder CLI agent for every request. It is intended for personal development, homelabs, and private deployments. Qoder is the only supported upstream at this time.

> [!IMPORTANT]
> CLI2API is not affiliated with or endorsed by Qoder. Use only accounts you are authorized to use, and follow the terms of Qoder and related services.

## How it works

```text
OpenAI client
  -> Go API + SQLite account control plane
    -> one isolated Node worker per Qoder account
      -> Qoder cloud HTTP/SSE API
```

Each enabled account gets its own Node process and private runtime HOME so Qoder WASM state is not shared across accounts. Go owns persistence, scheduling, concurrency limits, cooldowns, failover, and child-process lifecycle.

## Features

- OpenAI-compatible `POST /v1/chat/completions`
- Streaming and non-streaming responses
- Tool calls and `reasoning_content`
- Multiple Qoder accounts with pinning, routing, cooldown, and failover
- Browser Device Flow OAuth, PAT, and `qoder-native-v1` credential import/export
- Built-in React + Tailwind + HeroUI console with light and dark themes
- Single-container Docker Compose deployment
- Persistent SQLite database and credentials with ephemeral per-account runtime homes
- GitHub Actions for tests, container builds, and GHCR releases

![CLI2API console](docs/assets/console.png)

## Quick start

### Docker Compose

Requirements: Docker, Docker Compose, and a Qoder account you control.

The one-command launcher creates `deploy/.env` when needed, starts the published image (or builds locally if it is unavailable), and prints the generated API key on the first run:

```bash
git clone https://github.com/caigee-cmd/cli2api.git
cd cli2api
./scripts/start.sh
```

If you prefer to start Compose manually, leave `PROXY_API_KEY` blank. The service generates a cryptographically random key and stores it in SQLite on first startup; it is printed once in the container log. Save it before configuring clients.

Open `http://127.0.0.1:3010`, sign in with the printed key, and add Qoder accounts from **Accounts**.

The default Compose file publishes only `127.0.0.1:3010`; it does not expose the service publicly. Follow logs with:

```bash
docker compose logs -f qoder-api-proxy
```

Qoder login credentials created through the console are stored in SQLite under `account_credentials`. Workers materialize the encrypted Qoder auth files only in an ephemeral per-account runtime directory while running; the Docker deployment mounts that directory as tmpfs.

### Connect an OpenAI client

Configure your client with:

```text
Base URL: http://127.0.0.1:3010/v1
API Key:  <PROXY_API_KEY>
```

Or make a direct request:

```bash
export PROXY_API_KEY='paste-the-key-printed-on-first-start'

curl http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "messages": [{"role": "user", "content": "Reply with OK only"}],
    "stream": false
  }'
```

Pin a request to a specific account with `X-Qoder-Account: acc_...`. Without that header, the scheduler selects a ready account.

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `PROXY_API_KEY` | optional | First-run bootstrap value; blank generates and stores the key in SQLite |
| `QODER_DATA_DIR` | `/data` | SQLite database and durable account credentials |
| `QODER_RUNTIME_DIR` | `/run/cli2api` | Ephemeral per-account Qoder runtime homes |
| `QODER_MAX_INFLIGHT` | `4` | Maximum concurrent requests per account |
| `QODER_WORKER_BASE_PORT` | `32100` | Internal worker port range |

The API key is authoritative in SQLite. `PROXY_API_KEY` is only an optional bootstrap value and is ignored after a key already exists in the database.

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
- [Redacted protocol notes](docs/capture-notes.md)
- [Docker Compose deployment](deploy/README.md)
- [Changelog](CHANGELOG.md)
- [Security policy](SECURITY.md)

## Security and privacy

- Never expose the service without `PROXY_API_KEY`.
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
