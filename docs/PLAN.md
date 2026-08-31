# CLI2API Plan

last-updated: 2026-08-29

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
- Cursor / other non-Qoder CLI providers in this milestone
- Commit host IPs, passwords, auth blobs, or `docs/PRIVATE_DEPLOYMENT.md`

## Status

| Phase | Result |
|-------|--------|
| A–C | Protocol, non-stream MVP, streaming / tools / reasoning / console |
| D | Upstream usage, process-isolated pool, skip-main wasm boot |
| E | Open-source clone-and-run; `v0.1.0` at `eaf81ad` |
| F | Error taxonomy, account failover, console sign-in, Accounts login |
| G | Replaced by Phase H SQLite account control plane |
| H–K | SQLite control plane, protocol notes, logs, quota display |
| L | Qoder CN (`qoder` + `region=cn`) — code complete, L6 acceptance pending |
| M–O | Sticky routing, WorkBuddy quota ops, channel expansion — planned, not started |

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
- [x] Split add-account into type/name then login; hide concurrency and priority behind advanced options
- [x] Persist and edit account name, max inflight, priority, and WorkBuddy drop-system-prompt from the console
- [x] Filter the Models catalog by provider and paginate the list
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

- [x] Expose the build version and update directly to the latest stable GitHub release
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
- [x] Publish bilingual GitHub release notes from CHANGELOG.md

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
## Phase J — request history and runtime logs

Goal: give the console a short ops surface for recent chat requests and live process
output without copying sub2api billing, audit-body storage, or ops preaggregation.

- [x] Persist request metadata and failover attempts in SQLite
- [x] Capture Go/daemon stderr into an in-memory ring while keeping Docker logs
- [x] Expose authenticated `/api/logs/requests` and `/api/logs/runtime`
- [x] Add console `/logs` with Request history and Runtime tabs
- [x] Purge request history at 7 days or 20_000 rows
- [x] Paginate request history and filter by time, account, model, stream, and error kind
- [x] Paginate runtime logs newest-first with `limit` / `offset`

## Phase K — account quota display

Goal: show each Qoder account's remaining credits on the account card. Display-only;
no scheduling, billing, or persistence changes.

- [x] Capture the qodercli `qoderApi` singleton via a `quotaApi` needle in `worker/src/compat.mjs` (fail loudly on mismatch)
- [x] Expose authenticated `GET /admin/quota` on each daemon (Bearer token only, no WASM encode)
- [x] Go `refreshOne` fetches quota for hot/ready accounts; failures never affect readiness or cooldown
- [x] `AccountView` carries an optional `quota` snapshot through `/api/accounts` and `/api/overview`
- [x] Account card renders quota progress bar + add-on line (danger at exceeded, warning at ≥80%)
- [x] Worker compat, Go manager, and frontend coverage

## Phase L — Qoder CN (China mainland)

Goal: add Qoder China as `provider=qoder` + `region=cn`. Reuse the existing
child-process worker and `qoder-native-v1` credentials. Pin a second CLI,
`@qodercn-ai/qoderclicn@1.1.32` (both CLIs move in lockstep — see
`docs/PROVIDERS.md`), instead of a new protocol adapter.

Design facts and locked product decisions: `docs/PROVIDERS.md` section
「Qoder CN（中国大陆版）」. Do not spawn a full `qoderclicn` per request.
Do not invent a `qodercn` provider family. (2026-08-28: both pins upgraded
together to 1.1.32 after re-verifying all needles; future upgrades must move
both CLIs in lockstep.)

### L0 descriptor

- [x] Register Qoder region `cn` on the existing Qoder descriptor
- [x] Accept `POST /api/accounts` with `provider=qoder, region=cn`
- [x] Keep historical empty region as `global`
- [x] Flip `TestStoreRejectsUnknownProviderAndRegion` so `qoder+cn` is valid
- [x] Do not merge L0 alone: an enabled CN account would still spawn the global CLI

### L1 worker CLI filename

- [x] Hook `qoderclicn.js` in `rewrite-loader.mjs` (same needles as the global CLI at the time — pinned 1.1.27 then, now 1.1.32 in lockstep; `includes("qodercli.js")` does **not** match `qoderclicn.js`)
- [x] Resolve default bundle paths for both packages in `daemon.mjs`
- [x] Choose `hotEndpoint` fallback from worker-only `QODER_SITE` or the loaded filename (`api1.qoder.sh` vs `gateway.qoder.com.cn`)
- [x] Do not use `QODERCLI_SITE` to flip CN/Global; that env does not change compile-time `Xi`

### L2 manager spawn

- [x] Add `QODERCNCLI_JS` / `QoderCNCLIPath`; missing CN path fails spawn, never falls back to global CLI
- [x] Keep `HOME={runtime}`. Pass CLI-readable dirs: `QODER_CONFIG_DIR={home}/.qoder` or `QODERCN_CONFIG_DIR={home}/.qoder-cn`. Do **not** rely on `QODER_HOME` — the pinned CLI ignores it (1.1.27 at the time of this note; both CLIs now pinned to 1.1.32)
- [x] `materializeHome` / `SyncCredential` take region and read/write `.qoder` or `.qoder-cn`
- [x] Export `qoder-native-v1` includes `provider` + `region`; import without region stays global for old bundles
- [x] Unit test: CN account env points at `qoderclicn.js` + `.qoder-cn`; global still uses `qodercli.js` + `.qoder`

### L3 same-region failover

- [x] Add `RegionFilter` to `RouteQuery`; treat empty item region as `global`
- [x] Thread region through `executor/chat.go` `pick` / `attemptsFor` / stream + non-stream loops, not only `pool.go`
- [x] After the first pick, lock that account's region for the rest of the request
- [x] Sticky-escape from a cooling pin stays in the same region
- [x] Tests: Global A 429 → Global B, never CN; pin-CN cooling must not land on Global
- [x] Merge before L4; mixed-pool UI without L3 will failover across sites

### L4 console

- [x] Add `qoder-cn` i18n keys and `AddAccountModal` option
- [x] `accountProviderLabel` distinguishes Qoder CN from Qoder Global
- [x] PAT helper text for CN points at `https://qoder.com.cn/account/integrations`

### L5 image and config

- [x] Install both `@qoder-ai/qodercli@1.1.32` and `@qodercn-ai/qoderclicn@1.1.32` in the image
- [x] Export `QODERCLI_JS` and `QODERCNCLI_JS`
- [x] README / CHANGELOG bilingual notes

### L6 acceptance

- [x] Browser/PAT login waits for worker `hasAuthManager` before proxying device-flow (first-click CN race)
- [ ] Empty CN account → browser or PAT login → chat `只回复OK`
- [ ] Import CN `qoder-native-v1` → daemon becomes hot
- [ ] Mixed pool: pin stays on the matching CLI; Global 429 does not hit CN
- [ ] Existing Qoder stream / tools / reasoning / usage / quota tests stay green
- [ ] WorkBuddy path unchanged

## WorkBuddy acceptance (J0–J4 implemented)

Code and httptest contract tests are done; design lives in `docs/PROVIDERS.md`.
Remaining work is real-account acceptance, which cannot be done without live
WorkBuddy logins:

- [ ] Empty account → select WorkBuddy CN/Global → browser login → chat `只回复OK`
- [ ] Import `workbuddy-oauth-v1` JSON (accessToken + uid) → chat without browser
- [ ] Two WorkBuddy accounts: A rate-limited → request succeeds through B
- [ ] With the cross-provider model pool setting enabled (default), bare
      `glm-5.2` lands on both Qoder and WorkBuddy families; Qoder fully down →
      failover to WorkBuddy in the same route pool

## Phase M — session-sticky routing

Goal: raise upstream session-cache hit rates by keeping one conversation on the
same account, without breaking failover. Reference: wild-work sticky routing
(`rockswang/wild-work`) and sub2api-style session hashing.

### M1 session key

- [ ] Accept an optional session key: explicit `X-CLI2API-Session` header or a hash of the first user message
- [ ] Store session → account affinity with a bounded TTL in SQLite or in-memory state
- [ ] Keep account pin (`X-Qoder-Account`) the highest-priority routing signal

### M2 scheduler integration

- [ ] Prefer the sticky account when ready; escape to same-region / same-provider pool candidates on cooldown or quota exhaustion
- [ ] Record the actually served account in request history for cache-hit analysis
- [ ] Never let sticky routing bypass concurrency limits or error classification

### M3 acceptance

- [ ] Same session key → same account across consecutive requests until failure
- [ ] Sticky account 429 → failover stays in the same region/provider family
- [ ] No session key → behavior is exactly today's round-robin

## Phase N — WorkBuddy quota operations

Goal: keep WorkBuddy credits fresh without turning Accounts into a second
workbuddy2api. Everything is per-account opt-in, default off, and failures must
never block or cool down the chat path. Protocol, product locks, and suggested
landing spots are in `docs/PROVIDERS.md` 「积分与签到」(survey expanded
2026-08-30 against workbuddy2api `92514d8`). Implemented after live WorkBuddy
acceptance. Do not copy their credit-sorted picker or jitter-free whole-hour
scheduler.

### N1 keepalive

- [x] Periodic token refresh ahead of upstream expiry for WorkBuddy accounts only
- [x] Optional ~22:00 local keepalive pass shared with the check-in loop
- [x] Qoder accounts are excluded — no equivalent upstream action exists today
- [x] Refresh failures log and retry; they never flip readiness or cooldown
      (session-dead `12153` still uses the existing auth disable path)

### N2 daily check-in

- [x] Account-level opt-in switch, default off, persisted in a **new** SQLite
      migration (`010_workbuddy_auto_checkin.sql`, column `workbuddy_auto_checkin`)
- [x] `Client.DailyCheckin` on `POST {billingBase}/v2/billing/meter/daily-checkin`
      with body `{}` and billing headers; httptest for success / already-checked-in /
      session dead
- [x] Scheduled runs at roughly 09:00 / 21:00 process-local time **with minute
      jitter**; cooldown accounts may check in, disabled/opt-out accounts skip
- [x] "Already checked in" stays a check-in-only miss: still refresh credits,
      never write chat cooldown
- [x] Check-in results surface on the account card without new billing storage
- [x] Manual "check in now" shares the same adapter method as the scheduler

### N3 credit refresh

- [x] Console-level batch credit refresh button reusing the Phase K quota pipeline
- [x] Quota failures stay display-only (Phase K contract), never routing signals
- [x] No rate-based account selection — keep round-robin + pin + failover

## Phase O — more upstream channels

Goal: after WorkBuddy live acceptance proves the in-process adapter extension
point, evaluate the next channels on the same axis. Cursor stays last — its
protocol is the dirtiest. Trae CN Solo is candidate #2: pure HTTP/SSE like
WorkBuddy, no Node/WASM worker needed. Design: `docs/PROVIDERS_TRAE_SOLO.md`.
Do not implement from superseded `docs/PROVIDERS_TRAE.md`. Do not spawn
official `traecli`.

- [x] Trae CN Solo protocol survey: login, `llm_utils_chat` / `solo_work_lite`, credits (`docs/PROVIDERS_TRAE_SOLO.md`)
- [ ] WorkBuddy live acceptance complete (precondition)
- [x] Trae CN Solo T0–T4 in-process adapter (`internal/providers/trae/`; live T5 still needs a consumer Trae CN account)
- [ ] Trae CN Solo T5 live acceptance (`只回复OK`, catalog, failover, tools)
- [ ] Cursor provider only after Phase I canonical contract is stable

## Later

- Exact tokenizer matching if Qoder starts returning richer usage
- In-process multi-account (still impossible for Qoder WASM)
- Anthropic `/v1/messages` (see Phase I; kept here as the standing long-horizon item)
- Optional truncated prompt/completion capture behind an explicit switch

WorkBuddy J0–J4 are implemented; remaining WorkBuddy work is live-account
acceptance, not a new design doc. Qoder CN design lives in `docs/PROVIDERS.md`.
Trae CN Solo design lives in `docs/PROVIDERS_TRAE_SOLO.md`. Do not add extra
plan files. Do not start Cursor until Phase L is accepted or explicitly
deferred.
