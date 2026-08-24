# CLI2API Plan

last-updated: 2026-08-24

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
- Public exposure without the generated API key
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
- [x] Require the generated API key for every account and credential operation
- [x] Persist per-model context-window defaults and edit them from the Models page
- [x] Normalize public model IDs while preserving Qoder display names and internal routing keys

### H5 single-container deployment

- [x] Build Go, Node, pinned qodercli, worker sources, and frontend into one image definition
- [x] Replace the two-service Compose stack with one `qoder-api-proxy` service
- [x] Persist `/data/qoder.db` and account runtime state in one private volume
- [x] Document and implement migration from the existing mounted `.qoder` login

### H6 safe managed update

- [x] Expose the build version and select only the immediate next stable GitHub release
- [x] Delegate Docker replacement to a host updater over Unix Socket on Linux or authenticated loopback HTTP on macOS/Windows
- [x] Pause new API traffic, drain in-flight requests, and snapshot SQLite before replacement
- [x] Preserve and verify the existing /data mount across container recreation
- [x] Preserve existing Docker network attachments across update and rollback recreation
- [x] Restore the previous image and SQLite snapshot when health checks fail
- [x] Track immutable SQLite migrations by filename and SHA256 checksum
- [x] Publish multi-architecture `linux/amd64` and `linux/arm64` images
- [x] Publish checksum-verified updater assets for Linux, macOS, and Windows on `amd64` and `arm64`
- [x] Validate Windows native builds, updater tests, and PowerShell syntax in CI
- [x] Automate next-patch calculation, draft staging, image publication, and final Release publication

### H7 acceptance

- [ ] Empty install → create account → browser login → chat `只回复OK`
- [ ] Import `qoder-native-v1` → daemon becomes hot without browser login
- [ ] Two accounts → rate-limit A → request succeeds through B
- [x] Restart container → enabled accounts and credentials recover from SQLite
- [x] Existing non-stream, stream, tools, reasoning, model mapping, and usage tests pass

## Phase I — protocol adapter boundary

Goal: add Anthropic Messages without coupling public protocols to qodercli internals,
while keeping the current OpenAI Chat Completions path stable.

### I1 canonical conversation contract

- [ ] Define one internal conversation representation for text, thinking, images, tool calls/results, cache metadata, stop reasons, and provider IDs
- [ ] Preserve reversible mappings between client tool IDs and Qoder IDs
- [ ] Add fixtures for OpenAI Chat and Anthropic Messages conversations

### I2 adapter boundaries

- [ ] Keep `/v1/chat/completions` as an OpenAI-native ingress adapter
- [ ] Add `/v1/messages` as an Anthropic-native ingress adapter
- [ ] Route both adapters through the canonical conversation contract
- [ ] Keep Qoder payload construction in one Qoder upstream adapter
- [ ] Keep SSE response mapping protocol-specific at the edge

### I3 qodercli compatibility

- [ ] Pin and inspect qodercli hooks before worker startup
- [ ] Add Qoder adapter contract tests for model parameters, tools, tool results, thinking, and context length
- [ ] Make qodercli upgrades change only the Qoder adapter unless a public protocol contract changes
- [ ] Fail loudly on incompatible Qoder request/response shapes

### I4 Anthropic Messages acceptance

- [ ] Support system, text, image, thinking, tool_use, and tool_result blocks
- [ ] Support streaming `message_start`, content block events, `message_delta`, and `message_stop`
- [ ] Verify Claude Code `/v1/messages` multi-turn tool workflows
- [ ] Verify OpenAI Chat behavior remains unchanged

`/v1/messages` remains a later milestone until the canonical contract and Qoder adapter
are isolated; do not duplicate Qoder request construction in a second handler.
## Later

- Cursor provider
- Exact tokenizer matching if Qoder starts returning richer usage
- In-process multi-account (still impossible)
- Anthropic `/v1/messages`
- sub2api-style session-hash sticky
