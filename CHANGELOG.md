# Changelog

## Unreleased

Qoder account-pool hardening (Phase F):

- Classify Qoder errors: quota stays on the current request; rate-limit / auth / not-ready / 5xx failover
- Supervisor buffers POST bodies, skips cooling workers, sticky-escapes pinned accounts, restarts crashed children
- Worker serializes WASM encode + rewarm, caps in-flight, returns `Retry-After` / `X-Qoder-Error-Kind`
- Console Accounts page shows hot / cooldown / in-flight / restarts
- Console uses a split sign-in page (PROXY_API_KEY as password) instead of a header key field
- Qoder device-flow / PAT login lives on the Accounts page, per worker
- Accounts page is a worker list with status, browser login, and PAT fallback
- Writing-code docs collapsed to AGENTS.md + docs/DESIGN.md + docs/PLAN.md

## 0.1.0 - 2026-08-22

First public Qoder-first tree:

- OpenAI-compatible proxy, hot WASM worker, React console, Docker Compose on loopback
- Process-isolated account pool and Accounts console page
- Prefer upstream nested SSE usage; estimate only as fallback
- Skip-main / pure-wasm worker boot
- Redacted protocol docs, CI, and API-key fail-fast
- Compose forwards `ALLOW_INSECURE_API_KEY` for local placeholder keys
