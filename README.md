# CLI2API

[中文](README_ZH.md)

An unofficial, self-hosted OpenAI-compatible API for your own **Qoder CLI login**.

CLI2API keeps Qoder authentication and WASM encoding warm instead of spawning a full
CLI agent for every request. The current release supports Qoder only.

```text
OpenAI client
  -> Go API + SQLite account manager
    -> one isolated Node daemon per enabled Qoder account
      -> Qoder cloud HTTP/SSE API
```

## Features

- `POST /v1/chat/completions`, streaming and non-streaming
- Tool calls and `reasoning_content`
- Multiple Qoder accounts with routing, cooldown and failover
- Browser device-flow OAuth, PAT, and `qoder-native-v1` import/export
- Built-in HeroUI console with light and dark themes
- One-container Docker deployment with persistent SQLite data

## Quick start

Requirements: Docker, Docker Compose, and a Qoder account.

```bash
git clone https://github.com/caigee-cmd/cli2api.git
cd cli2api/deploy
cp .env.example .env
```

Set a strong key in `deploy/.env`:

```env
PROXY_API_KEY=replace-with-a-random-secret
```

Start the service:

```bash
docker compose up -d --build
curl http://127.0.0.1:3010/health
```

Open `http://127.0.0.1:3010` and sign in with `PROXY_API_KEY`. Add Qoder accounts from
**Accounts**. The default Compose file only publishes `127.0.0.1:3010`.

If `${QODER_HOME:-$HOME/.qoder}` already contains a Qoder login, the first startup
imports it when the SQLite database is empty.

## API example

```bash
export PROXY_API_KEY='replace-with-a-random-secret'

curl http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "messages": [{"role": "user", "content": "Reply with OK only"}],
    "stream": false
  }'
```

Clients can pin an account with `X-Qoder-Account: acc_...`. Without it, the Go
scheduler selects a ready account.

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `PROXY_API_KEY` | required | Protects the console, `/api/*`, and `/v1/*` |
| `QODER_DATA_DIR` | `/data` | SQLite database and account runtime homes |
| `QODER_HOME` | host `~/.qoder` | Optional one-time import source |
| `QODER_MAX_INFLIGHT` | `4` | Per-account request limit |
| `QODER_WORKER_BASE_PORT` | `32100` | Internal daemon port range |

Placeholder keys such as `dev-key` require `ALLOW_INSECURE_API_KEY=1` and should never
be used on a reachable host.

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | Public health probe |
| `GET` | `/v1/models` | Model catalog |
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat |
| `GET/POST` | `/api/*` | Console API |

## Account model

Qoder WASM state is process-global, so every enabled account receives its own Node
process and private HOME. Go owns SQLite persistence, scheduling, cooldown, failover,
and child lifecycle.

Raw credentials are never returned by normal account APIs. Credential export is an
explicit action and should be treated as sensitive.

## Development

```bash
go test ./...
cd worker && npm test
cd ../frontend && npm ci && npm run build && npm run lint
```

After frontend changes, run `cd frontend && npm run sync` to update the embedded assets.
See [CONTRIBUTING.md](CONTRIBUTING.md) for repository rules.

## Security and scope

- Personal/self-hosted use with accounts you control
- Do not expose the service without `PROXY_API_KEY`
- Do not commit `.qoder`, auth blobs, tokens, cookies, or raw captures
- This project is not affiliated with or endorsed by Qoder
- Upstream API or CLI changes may break compatibility; qodercli is pinned and checked
  at startup

See [SECURITY.md](SECURITY.md) for reporting guidance.

## Documentation

- [Architecture and behavior](docs/DESIGN.md)
- [Current milestone](docs/PLAN.md)
- [Redacted protocol notes](docs/capture-notes.md)
- [Docker deployment](deploy/README.md)

## License

[MIT](LICENSE)
