# CLI2API Plan

last-updated: 2026-08-23

Qoder-first OpenAI-compatible proxy. Cursor and other CLIs wait until the current Qoder milestone is done.

Canonical files:

- Rules: `AGENTS.md`
- Design / login / routing / console: `docs/DESIGN.md`
- This checklist: `docs/PLAN.md`

Do not add extra plan files.

## Boundary

Do:

- Reuse local Qoder login state
- Serve `POST /v1/chat/completions`
- Keep a hot `QoderContext` and call cloud HTTP/SSE directly
- Self-host on a private Docker network
- Keep iterating Qoder login, usage, and account routing
- Borrow scheduling ideas from [sub2api](https://github.com/Wei-Shaw/sub2api), not its commercial gateway

Do not:

- Spawn a full `qodercli` agent per request
- Public exposure without `PROXY_API_KEY`
- Commercial multi-user resale of one login
- Cursor / other CLI providers in this milestone
- Commit host IPs, passwords, auth blobs, or `docs/PRIVATE_DEPLOYMENT.md`

## Status

| Phase | Result |
|-------|--------|
| A–C | Protocol, non-stream MVP, streaming / tools / reasoning / console |
| D | Upstream usage, process-isolated pool, skip-main wasm boot |
| E | Open-source clone-and-run; `v0.1.0` at `eaf81ad` |
| F | Error taxonomy, account failover, console sign-in, Accounts login |
| G | Replaced by Phase H SQLite account control plane |

Typical small-chat latency is ~1-2s after warmup, versus ~10s+ for spawn-CLI wrappers.

## Phase H — SQLite account control plane

Goal: replace environment-defined workers and the Node supervisor with a durable,
Sub2API-style SQLite account registry while preserving the working Qoder execution path.

### H1 account database

- [x] Add pure-Go SQLite dependency and migrations
- [x] Persist account metadata, native credential blobs, UID, status, and cooldown
- [x] Add create, update, enable, disable, delete, import, and export repository tests
- [x] Keep SQLite and account files under one configurable `/data` directory

### H2 process isolation

- [x] Go starts one `worker/src/daemon.mjs` child per enabled account
- [x] Materialize each account into a private HOME before spawn
- [x] Capture health, UID, in-flight count, restarts, and last error
- [x] Sync OAuth/PAT credential changes back into SQLite
- [x] Stop and clean account runtime safely on disable/delete

### H3 single scheduler

- [x] Make Go the only round-robin, pinning, cooldown, and failover owner
- [x] Remove Node supervisor account selection
- [x] Preserve quota vs rate-limit vs auth vs not-ready vs unavailable behavior
- [x] Preserve `X-Qoder-Account` on normal and streaming responses

### H4 management API and console

- [x] Add account CRUD endpoints
- [x] Add browser OAuth, PAT, native credential import/export endpoints
- [x] Add account creation and import UI using HeroUI only
- [x] Show UID, auth type, enabled state, runtime status, cooldown, and last error
- [x] Require `PROXY_API_KEY` for every account and credential operation
- [x] Persist per-model context-window defaults and edit them from the Models page
- [x] Normalize public model IDs while preserving Qoder display names and internal routing keys

### H5 single-container deployment

- [x] Build Go, Node, pinned qodercli, worker sources, and frontend into one image definition
- [x] Replace the two-service Compose stack with one `qoder-api-proxy` service
- [x] Persist `/data/qoder.db` and account runtime state in one private volume
- [x] Document and implement migration from the existing mounted `.qoder` login

### H6 acceptance

- [ ] Empty install → create account → browser login → chat `只回复OK`
- [ ] Import `qoder-native-v1` → daemon becomes hot without browser login
- [ ] Two accounts → rate-limit A → request succeeds through B
- [x] Restart container → enabled accounts and credentials recover from SQLite
- [x] Existing non-stream, stream, tools, reasoning, model mapping, and usage tests pass

