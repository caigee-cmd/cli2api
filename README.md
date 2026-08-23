# CLI2API

> Current codebase focuses on the **Qoder** provider (`qoder-api-proxy` worker/proxy). Repo name is `cli2api` for the broader CLI-login-to-API direction.

中文文档：[README_ZH.md](README_ZH.md)

OpenAI-compatible proxy that reuses a **local Qoder CLI login state** and calls Qoder cloud APIs directly.

This is **not** a `spawn qodercli` wrapper like [avaritiachaos/qoder-proxy](https://github.com/avaritiachaos/qoder-proxy) / [foxy1402/qoder-proxy](https://github.com/foxy1402/qoder-proxy).  
Architecture is closer to [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI): keep auth warm, translate protocols, execute upstream HTTP/SSE.

Personal / self-hosted only. Pin `@qoder-ai/qodercli@1.1.27`; mismatch exits loudly. Cursor is out of scope for v0.1.

```text
Client (OpenAI SDK / Codex / CherryStudio)
  -> qoder-api-proxy (:3010, Go + SQLite control plane)
    -> one isolated Node daemon per enabled account
      -> https://api1.qoder.sh/.../agent_chat_generation?Encode=1
```

## Console UI

React + Tailwind CSS v4 + **HeroUI** console. Layout/taste notes: [`docs/DESIGN.md`](docs/DESIGN.md). Agent rules: [`AGENTS.md`](AGENTS.md). Current work: [`docs/PLAN.md`](docs/PLAN.md).

Account routing borrows from [sub2api](https://github.com/Wei-Shaw/sub2api); we do not copy its commercial gateway.

```text
frontend/
  src/
    components/layout/   # AppLayout / AppSidebar / AppHeader
    pages/               # Login / Overview / Accounts / Models / Access
    api/ hooks/ i18n/
```

Build & embed into Go:

```bash
cd frontend && npm install && npm run sync
# copies dist -> internal/webui/static
```

Docker multi-stage build already compiles the frontend before the Go binary.

## Features

- OpenAI-compatible `POST /v1/chat/completions`
- Streaming SSE (`text/event-stream`) and non-streaming JSON
- Tool calling: request `tools` / `tool_choice`, response `tool_calls`
- Thinking / reasoning passthrough (`reasoning_content`) when upstream provides it
- Model alias mapping (`qwen3.7-plus -> qmodel`, etc.)
- Auth self-heal (`/admin/rewarm`, auto-rewarm on auth failures)
- Docker Compose deployment (internal network friendly)
- Prefers upstream usage / credits when nested SSE returns them; estimates only as fallback

## Why this exists

Typical CLI-wrapper proxies are slow because each request cold-starts the agent CLI (often ~10s+) and may expose a local workspace directory.

This project keeps a warm WASM/auth context and talks to the cloud infer endpoint directly, so latency is closer to upstream inference.

## Requirements

- Docker + Docker Compose for the recommended deployment
- A real `PROXY_API_KEY`
- Optional existing `~/.qoder` login for one-time migration

## Quick start

```bash
cd deploy
cp .env.example .env
# set a real PROXY_API_KEY
docker compose up -d --build
```

The single container includes Go, Node, pinned qodercli, and the frontend. SQLite and
per-account homes persist in the `qoder-data` volume. Open `/accounts` to add browser
OAuth, PAT, or `qoder-native-v1` accounts.

Important variables:

| Var | Meaning |
|-----|---------|
| `PROXY_API_KEY` | Console and `/v1` API key |
| `QODER_DATA_DIR` | SQLite and runtime root; `/data` in Docker |
| `QODER_HOME` | Optional existing login imported once when SQLite is empty |
| `QODER_WORKER_BASE_PORT` | Internal daemon port range start |

### Test

```bash
curl -s http://127.0.0.1:3010/health

curl -s http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "stream": false,
    "messages": [{"role":"user","content":"Reply with OK only"}]
  }'
```

Streaming:

```bash
curl -N http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "stream": true,
    "enable_thinking": true,
    "messages": [{"role":"user","content":"12*8=？只要答案"}]
  }'
```

Tool calling:

```bash
curl -s http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "stream": false,
    "tool_choice": "auto",
    "tools": [{
      "type": "function",
      "function": {
        "name": "exec_command",
        "description": "Run a shell command",
        "parameters": {
          "type": "object",
          "properties": {"cmd": {"type": "string"}},
          "required": ["cmd"]
        }
      }
    }],
    "messages": [
      {"role":"system","content":"Use tools when needed. Do not put fake tool calls in plain text."},
      {"role":"user","content":"Use a tool to run pwd"}
    ]
  }'
```

## Web UI

Open the built-in management console after starting the proxy:

```bash
open http://127.0.0.1:3010/
```

Open `/login` and sign in with `PROXY_API_KEY` (console password). Console APIs (`/api/*`) and `/v1` both require it when the key is set. Qoder device-flow / PAT login is on the Accounts page, one worker at a time.

Pages:

- Overview: runtime status + endpoints
- Accounts: Qoder login + process-isolated worker pool
- Models: current catalog
- Access: curl snippet + quick chat test

Source lives in `frontend/` and is embedded via `internal/webui`.

## Docker

```bash
cd deploy
cp .env.example .env   # set PROXY_API_KEY
docker compose up -d --build
```

Notes:

- Default compose creates a private `cli2api` network and publishes `127.0.0.1:3010` only.
- Worker needs access to Qoder login files (typically mount `~/.qoder`).
- Sample plaintext template is `worker/last-plain.sample.json`; override with `PLAIN_TEMPLATE_PATH` if you have a richer capture.

See [`deploy/README.md`](deploy/README.md).

## API surface

| Method | Path | Notes |
|--------|------|------|
| GET | `/health` | Proxy health |
| GET | `/v1/models` | Static/known aliases |
| POST | `/v1/chat/completions` | OpenAI chat completions |
| GET | `/debug/auth-snapshot` | Auth snapshot (key required) |
| GET | `/debug/endpoints` | Resolved endpoints (key required) |
| GET/POST | `/api/*` | Console APIs (same key as `/v1`) |
| POST | worker `/admin/rewarm` | Force auth/context refresh |

### Thinking / reasoning

Enable with any of:

- `is_reasoning: true`
- `enable_thinking: true`
- `enable_reasoning: true`
- `thinking: ...`
- `reasoning_effort: "low|medium|high"`

Streaming returns `delta.reasoning_content` when upstream provides it.  
Non-streaming returns `message.reasoning_content`.

### System prompt behavior

Worker **does not** inherit the capture-template Qoder agent system prompt/tools (those are huge and break multiturn).

It **does** preserve caller system prompts from:

- top-level `system`
- `messages[].role = system|developer`

So identity prompts injected by your gateway remain intact.

### Usage

If nested SSE includes `usage` or `llm_model_result`, that is used as-is (`usage.source=upstream`).  
Otherwise the proxy estimates tokens (`usage.source=estimate`) so billing still has a number:

- non-stream: `usage`
- stream: final usage chunk before `[DONE]`

### Multi-account

Accounts are stored in SQLite and managed from the **Accounts** page. You no longer
configure `QODER_HOMES`, worker URLs, or account IDs.

Each enabled account still gets an isolated Node daemon and HOME because Qoder WASM is
process-global. Go is the only scheduler and handles round-robin, pinning, cooldown, and
failover. Supported onboarding methods:

- browser device-flow OAuth
- PAT
- `qoder-native-v1` JSON import/export

On the first single-container start, an existing mounted `QODER_HOME` is imported into
SQLite automatically when the database is empty. Pin requests with `X-Qoder-Account`.

## Project layout

```text
cmd/server/          Go entrypoint
frontend/            React console
internal/api/        HTTP handlers
internal/auth/       Login-state helpers
internal/endpoint/   Endpoint resolution
internal/executor/   Proxy -> worker calls
internal/translate/  OpenAI request structs
internal/webui/      Embedded console assets
worker/              Hot QoderContext auth/encode worker
deploy/              Docker assets
docs/                DESIGN.md, PLAN.md, protocol notes
testdata/            Redacted samples
```

## Docs

- [`AGENTS.md`](AGENTS.md) — hard rules for agents
- [`docs/DESIGN.md`](docs/DESIGN.md) — architecture, login, routing, console, design system
- [`docs/PLAN.md`](docs/PLAN.md) — current milestone checklist
- [`docs/capture-notes.md`](docs/capture-notes.md) — redacted protocol notes
- [`docs/next-prepareRequest.md`](docs/next-prepareRequest.md) — worker boot notes
- [`CONTRIBUTING.md`](CONTRIBUTING.md) / [`SECURITY.md`](SECURITY.md)
- Local private ops notes (gitignored): `docs/PRIVATE_DEPLOYMENT.md`

## Compliance / warning

Intended for **personal / self-hosted** use of your own Qoder login.

Do **not**:

- expose this publicly without auth
- share one login across many users commercially
- commit raw auth blobs, tokens, or capture dumps

Upstream ToS / account risk is yours to evaluate.

## Status

Usable for self-hosting:

- non-stream + stream
- tool calls
- reasoning passthrough
- Docker network deployment
- auth rewarm/self-heal

Still evolving:

- Cursor provider

## License

MIT. See [LICENSE](LICENSE).
