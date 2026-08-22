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
| F | Error taxonomy, supervisor failover, console sign-in, Accounts login |
| G | Current: keep polishing Qoder login, routing, menu, HeroUI console |

Typical small-chat latency is ~1-2s after warmup, versus ~10s+ for spawn-CLI wrappers.

## Phase G — Qoder product loop

Goal: the self-hosted console and pool feel like a real ops tool, not a lab page. Keep tightening login, distribution, and IA. HeroUI only. Taste v1 adapted in `docs/DESIGN.md`.

### G0 docs home

- [x] `AGENTS.md` = hard rules + pointer to DESIGN/PLAN
- [x] `docs/DESIGN.md` = architecture, two logins, routing, console IA, design system
- [x] `docs/PLAN.md` = current checklist only
- [x] README / frontend README / docs index point at those three files

### G1 login flow

Console password (`PROXY_API_KEY`) and Qoder login stay separate.

- [x] `/login` split page; not a header key field
- [x] Qoder device-flow / PAT on `/accounts` per worker
- [ ] Device-flow status should stay attached to the worker being signed in (no cross-account bleed)
- [ ] After Qoder login, Accounts should show signed-in / cooling without a manual refresh hunt
- [ ] Empty / error / pending states for each worker, including “browser closed, try PAT”

### G2 usage and distribution

Keep process isolation. Improve the pool, do not invent in-process multi-account.

- [x] Classify quota vs rate-limit vs auth vs not-ready vs 5xx
- [x] Supervisor failover + sticky-escape + child restart
- [x] `QODER_MAX_INFLIGHT` + WASM encode lock
- [ ] Console can tell which account served a test chat (`X-Qoder-Account`)
- [ ] Health strip: ready / cooling / in-flight / last error, no host paths
- [ ] Document recommended `QODER_HOMES` vs separate worker containers in README only (no extra doc)

### G3 menu and console

Keep four nav items. Login is a gate.

- [x] Nav: Overview / Accounts / Models / Access
- [x] `/auth` redirects to `/accounts`
- [ ] Overview: signed-in worker count, not a second login form
- [ ] Access: account picker only when more than one worker exists
- [ ] No duplicate page titles under the shell header
- [ ] After UI edits, `cd frontend && npm run sync`

### G4 acceptance

- [ ] Local: console login → Accounts browser/PAT login → chat `只回复OK`
- [ ] Two workers: 429 on A, chat lands on B, pinned A sticky-escapes when cooling
- [ ] Do not tag until asked; `v0.1.0` stays as-is

## Later (not G)

- Cursor provider
- Exact tokenizer matching if Qoder starts returning richer usage
- In-process multi-account (still impossible)
- Anthropic `/v1/messages`
- sub2api-style session-hash sticky
