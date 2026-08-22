# CLI2API Plan

last-updated: 2026-08-22

Qoder-first OpenAI-compatible proxy. Cursor and other CLIs are later providers, not this milestone.

## Boundary

Do:

- Reuse local Qoder login state
- Serve `POST /v1/chat/completions`
- Keep a hot `QoderContext` and call cloud HTTP/SSE directly
- Self-host on a private Docker network
- Make the public GitHub repo installable by strangers

Do not (this milestone):

- Spawn a full `qodercli` agent per request
- Public exposure without `PROXY_API_KEY`
- Commercial multi-user resale of one login
- Cursor / other CLI providers
- Commit host IPs, passwords, auth blobs, or `docs/PRIVATE_DEPLOYMENT.md`

## Status

| Phase | Result |
|-------|--------|
| A protocol | Confirmed `COSY.*` request-scoped auth, `Encode=1` body, nested SSE |
| B non-stream MVP | Worker encodes via live WASM context; Go proxy fronts OpenAI JSON |
| C usable | Real streaming, tool calls, reasoning passthrough, rewarm/self-heal, React console |
| D hardening | Upstream usage when present, process-isolated account pool, skip-main wasm boot |
| E open-source | E0–E5 passed; `v0.1.0` tagged after green CI |

Typical small-chat latency is ~1-2s after warmup, versus ~10s+ for spawn-CLI wrappers.

## Runtime

```text
Client
  -> qoder-api-proxy (:3010)
    -> qoder-auth-worker (:3020, hot QoderContext)
      -> https://api1.qoder.sh/.../agent_chat_generation?Encode=1
```

Worker pins `@qoder-ai/qodercli@1.1.27` and patches WASM capture needles. If the CLI source no longer matches, startup fails with a version-aware error instead of running half-broken.

## Usage

Prefer nested SSE `usage` / `llm_model_result` when present.  
If upstream only reports credits, keep the local estimate and set `usage.source=estimate`.

## Accounts

Qoder WASM / AuthManager is process-global. Multi-account means one worker process per `HOME`.

- `QODER_HOMES=acc1=/root,acc2=/home/acc2` starts a supervisor
- Go proxy round-robins `QODER_WORKER_URLS` and fails over on 429/auth errors
- Clients can pin `X-Qoder-Account`
- Console `/accounts` shows the pool; do not expose host paths or tokens

## Auth

- Console `/api/*` and `/v1` require `PROXY_API_KEY` when it is set
- Worker `/admin/*` and chat require the same key
- `/health` stays open for probes

---

## Phase E — Open-source release

Goal: a stranger can clone [caigee-cmd/cli2api](https://github.com/caigee-cmd/cli2api), log in with their own Qoder CLI, and get a working OpenAI-compatible endpoint without private ops notes.

### E0 freeze D locally

Uncommitted D work must land before any public push:

- [x] worker usage extractor (`worker/src/usage.mjs`) + tests
- [x] process-isolated supervisor (`worker/src/supervisor.mjs`)
- [x] Go account pool (`internal/accounts`) + `/api/accounts`
- [x] skip-main / pure-wasm boot path
- [x] console Accounts page + leftover Vite assets removed
- [x] README / compose / `.env.example` already mention the above; keep them in the same commit

Do not include: `docs/PRIVATE_DEPLOYMENT.md`, `docs/private/`, host `.env`, auth blobs.

### E1 sanitize for strangers

- [x] Strip local-only paths from public files (`/tmp/qoder-wasm-spike`, capture preload scripts, machine names)
- [x] Worker template loader should use `PLAIN_TEMPLATE_PATH` + `worker/last-plain.sample.json` only
- [x] Rewrite `docs/capture-notes.md` / `docs/next-prepareRequest.md` as redacted protocol notes, not a personal lab log
- [x] Replace `frontend/README.md` Vite boilerplate with console build/sync notes
- [x] README EN/ZH: add Accounts page, `usage.source`, supervisor boot, ToS warning above the fold
- [x] Secret scan: no tokens, cookies, account ids, host IPs, customer prompts in git tree or history

### E2 make clone → run boring

- [x] One-command local path in README: worker `npm start` + `go run ./cmd/server`
- [x] Compose: document Qoder HOME mount, `PROXY_API_KEY`, optional `QODER_HOMES`
- [x] Add container healthchecks for proxy + worker
- [x] Fail fast if `PROXY_API_KEY` is empty/`change-me` in non-dev
- [x] Pin Node 20 / Go 1.25 in README and Dockerfiles
- [x] Keep sample plaintext template tiny and non-secret

### E3 CI and release hygiene

- [x] GitHub Actions: `go test ./...`, `cd worker && npm test`, `cd frontend && npm ci && npm run build`
- [x] Optional: `gitleaks` or equivalent on PRs
- [x] `CHANGELOG.md` starting at the first public tag
- [x] Issue / PR templates: bug, pin-mismatch
- [x] `CONTRIBUTING.md` + `SECURITY.md` (private disclosure, no raw auth dumps)
- [x] Tag `v0.1.0` after second-machine clone check and green CI

### E4 positioning, not extra features

README first screen already says:

- [x] Not a spawn-CLI wrapper
- [x] Warm WASM context + direct cloud HTTP/SSE
- [x] Personal / self-hosted only
- [x] Qoder CLI pin is a hard dependency; mismatch exits loudly
- [x] Cursor is explicitly out of scope for v0.1

Nice-to-have after v0.1, not blockers:

- screenshot of the console
- `make test` / `make up`
- GitHub Discussion / Discord
- model-list live refresh

### E5 acceptance

Checked 2026-08-22 on a clean local clone and us1 compose:

- [x] `cp .env.example .env` and set `PROXY_API_KEY`
- [x] mount a real `~/.qoder` login
- [x] `docker compose up -d --build`
- [x] `GET /health` 200
- [x] `POST /v1/chat/completions` with the key returns a chat (`OK`, `usage.source=upstream`)
- [x] local `go test ./...` + worker tests + gitleaks on the clone
- [x] `git grep` finds no host IPs / auth blobs / private deploy notes

Tagged `v0.1.0` at `eaf81ad` after GitHub Actions went green.

---

## Phase F — Qoder account pool hardening

Goal: keep the v0.1 personal/self-hosted shape, but make multi-account auth and failover actually hold under Qoder's real errors. Borrow scheduling ideas from [sub2api](https://github.com/Wei-Shaw/sub2api), not its commercial gateway (billing, Redis slots, sticky-for-profit, multi-tenant keys).

Do:

- Classify upstream errors instead of treating every failure as a 500
- Fail over only when another account can reasonably succeed
- Health-skip / restart isolated workers
- Show cooldown state in the console
- Keep process isolation (one Qoder HOME / WASM context per worker)

Do not:

- In-process multi-account
- Copy sub2api billing / user API keys / Redis concurrency
- Long cooldown on `insufficient_quota` (Qoder often means this prompt is too large, not empty balance)
- Cursor / Anthropic providers

### F0 error taxonomy

Qoder errors are not all account-level:

| Kind | Typical signal | HTTP | Fail over | Account cooldown |
|------|----------------|------|-----------|------------------|
| quota | `insufficient_quota`, `#token-limit`, oversized prompt | 429 | no | no |
| rate_limit | `response code=429`, too many requests | 429 | yes | ~60s, honor `Retry-After`, cap 10m |
| auth | 401/403, unauthorized, FORBIDDEN | 401/403 | yes | ~30s + existing rewarm |
| not_ready | hot context missing | 503 | yes | ~10s |
| unavailable | transport / 5xx | 502/503 | yes | ~15s |

Worker must return that status (not a blanket 500) so Go and supervisor can see it.

### F1 worker stability

- [x] Serialize WASM `prepareInferRequest` + rewarm on one lock; do not hold it across upstream fetch
- [x] Optional `QODER_MAX_INFLIGHT` (default 4) so one login does not stampede Qoder
- [x] `/health` reports `inFlight`, `lastError`, `rewarmCount`
- [x] Map thrown errors through the F0 classifier and set `Retry-After` when cooling down

### F2 supervisor (default compose path)

Default compose has **one** worker URL. Go cannot fail over unless the supervisor does.

- [x] Buffer POST bodies so a failed child can be retried
- [x] Skip children that are down / not hot; sticky-escape a pinned `X-Qoder-Account` when that child is cooling
- [x] Restart exited children with backoff
- [x] `/health` and `/admin/accounts` list id/ready/hot/down_until/last_error — **no host paths**
- [x] Honor `X-Qoder-Account` and echo it on the response

### F3 Go pool

- [x] Shared classifier + `Retry-After`
- [x] `MarkOK` after a successful chat
- [x] Do not burn the rest of the pool on quota / oversized-prompt errors
- [x] Merge supervisor health into `/api/accounts` + overview

### F4 console + docs

- [x] Accounts page: ready/hot, cooldown, last error, in-flight, restarts
- [x] Access/Auth: pin account on test chat / login
- [x] Console sign-in page; Qoder login moved onto Accounts
- [x] README: supervisor `QODER_HOMES` vs separate `QODER_WORKER_URLS` containers

### F5 acceptance

- [x] Worker + Go unit tests for classify / pick / failover / no-failover-on-quota
- [x] Local: two fake workers, 429 on A, chat on B
- [x] Do not tag until this lands; v0.1.0 stays as-is

## Later (not F)

- Cursor provider (separate milestone)
- Exact tokenizer matching if Qoder starts returning richer usage
- In-process multi-account is still impossible; keep process isolation
- Anthropic `/v1/messages`
- sub2api-style session-hash sticky (optional after F; chat is already fully in-body)
ges`
- sub2api-style session-hash sticky (optional after F; chat is already fully in-body)
