# DESIGN

last-updated: 2026-08-22

Canonical design and product notes for agents. Plans live in `docs/PLAN.md`. Hard rules live in `AGENTS.md`.

Reference backend: [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api).  
We keep borrowing **account routing, failover, cooldown, in-flight caps**.  
We do **not** copy billing, Redis concurrency, multi-tenant keys, or commercial sticky sessions.

Console taste: [design-taste-frontend-v1](https://github.com) adapted for a self-hosted ops console. Components **must** be HeroUI.

## Runtime

```text
Client (OpenAI SDK / Codex / CherryStudio)
  -> qoder-api-proxy (:3010)            # one container
    -> Go control plane                  # auth, SQLite, routing, console
      -> Node daemon per account         # isolated HOME + hot QoderContext
        -> https://api1.qoder.sh/.../agent_chat_generation?Encode=1
```

Worker pins `@qoder-ai/qodercli@1.1.27`. Needle mismatch exits loudly.

The request path that builds plaintext payloads, calls Qoder WASM encode, forwards
HTTP/SSE, parses tools/reasoning, and resolves usage remains unchanged. The account
control plane may be replaced; the proven Qoder execution path must not be rewritten.

## Protocol adapters

Public protocols and the Qoder upstream format are separate contracts. Do not let
OpenAI Chat Completions or Anthropic Messages handlers build Qoder payloads independently.

```text
/v1/chat/completions -> OpenAI adapter    -> canonical conversation
/v1/messages         -> Anthropic adapter -> canonical conversation
                                                   -> Qoder adapter
                                                   -> prepareInferRequest / Qoder cloud
```

The canonical conversation preserves text, thinking, tool calls/results, images, cache
metadata, stop reasons, and provider identifiers. Each public adapter owns only its
request validation and response/SSE mapping. The Qoder adapter owns pinned CLI/runtime
compatibility, model parameters, tool normalization, and upstream event mapping.

When qodercli changes, update the Qoder adapter and its version-aware compatibility
tests first; do not duplicate the change in both public protocol handlers. Keep
`/v1/chat/completions` stable while `/v1/messages` is added as a separate ingress.

`/v1/messages` should be implemented as a native Anthropic boundary, not as
Anthropic -> OpenAI -> Qoder string rewriting. Borrow sub2api's reversible tool
mapping, cross-turn state, orphan-result filtering, and history repair, but keep the
project's existing Qoder execution path and account model.
## Two logins

They are not the same password.

| Gate | Secret | Where | Unlocks |
|------|--------|-------|---------|
| Console | `PROXY_API_KEY` | `/login` | Overview, accounts, models, API test |
| Qoder | device-flow / PAT | `/accounts` per worker | Upstream chat for that HOME |

`/health` stays open. `/api/*`, `/v1`, worker `/admin/*` and chat require the console key when it is set.

The console API key is stored in SQLite `app_secrets` under `proxy_api_key`. A blank database generates a random key on first startup; `PROXY_API_KEY` is only an optional bootstrap value.

## Account routing

Qoder WASM / AuthManager is process-global. One HOME = one worker process.

SQLite is the durable account registry. Go owns the database, scheduling, cooldown,
failover, and child lifecycle. Node daemons never select another account.

The service starts one Node daemon per enabled account. Each daemon receives a private
ephemeral runtime HOME materialized from its SQLite credential record. The SQLite
credential record is authoritative; the runtime files are derived working copies.
`QODER_HOMES`,
`QODER_WORKER_URLS`, and `QODER_ACCOUNT_IDS` are removed from the product flow.

Supported account onboarding:

- Qoder browser device-flow OAuth
- Qoder PAT login
- `qoder-native-v1` JSON import containing the encrypted `.auth/user` blob and its
  matching `machine_id`

Arbitrary `access_token` / `refresh_token` JSON is not supported. Qoder credentials
also depend on private user material, organization data, encryption keys, and device
identity. The API never returns raw credentials except through the explicit export
action.

Clients may pin `X-Qoder-Account`. If that worker is cooling, sticky-escape to another ready worker.

Error taxonomy (do not treat every 429 as empty balance):

| Kind | Signal | HTTP | Fail over | Cooldown |
|------|--------|------|-----------|----------|
| quota | `insufficient_quota`, `#token-limit`, oversized prompt | 429 | no | no |
| rate_limit | generic 429 / too many requests | 429 | yes | ~60s, honor Retry-After, cap 10m |
| auth | 401/403, FORBIDDEN | 401/403 | yes | ~30s + rewarm |
| not_ready | hot context missing | 503 | yes | ~10s |
| unavailable | transport / 5xx | 502/503 | yes | ~15s |

`QODER_MAX_INFLIGHT` default 4. WASM encode + rewarm share one lock; do not hold it across upstream fetch.

Do not expose host paths or tokens in `/api/accounts`.

## Console IA

Keep the menu short. Login is a gate, not a nav item.

| Route | Nav | Job |
|-------|-----|-----|
| `/login` | no | Console password |
| `/` | Overview | Runtime pulse |
| `/accounts` | Accounts | Qoder login + pool |
| `/providers` | Models | Catalog + per-model context-window defaults |

Public model IDs are lowercase request identifiers. Qoder CLI names remain display labels, while internal Qoder keys are shown only for routing diagnostics.
| `/access` | Access | Base URL + quick chat |
| `/auth` | redirect | Legacy → `/accounts` |

Do not bring back a separate Auth page.

## Design system

Stack: React 19, Vite, Tailwind CSS v4, **HeroUI**. Icons currently `lucide-react` (already installed). Do not add shadcn / MUI / Ant.

Taste v1 dials, adapted for this console (not a marketing landing):

- `DESIGN_VARIANCE=6` — split/asymmetric, not centered hero
- `MOTION_INTENSITY=3` — hover/active only; no perpetual motion, no magnetic cursor
- `VISUAL_DENSITY=6` — ops density: status strips and lists, not gallery cards

Rules:

- One accent: emerald on zinc/charcoal (`#0b0f14`, `#12171d`, `#1f9d6a`)
- Type: Outfit + IBM Plex Mono. No Inter, no serif, no emoji
- Full-height: `min-h-dvh`, never `h-screen`
- Layout: CSS Grid. Login is split (copy left, form right). Lists use `divide-y` / `border-t`
- Cards only when grouping actions. Metrics are strips, not boxed KPI tiles
- Forms: label above, error below, helper text in markup
- Loading / empty / error states are mandatory
- Noise overlay stays on a fixed `pointer-events-none` layer
- Animate `transform` / `opacity` only

Banned: purple AI chrome, neon glow, gradient headlines, 3 equal feature cards, centered generic login card, fake 99.9% stats.

Copy voice: concrete. “Enter the console” / “Sign in with browser”. Not Seamless / Unleash.

## Files

| Path | Owns |
|------|------|
| `frontend/src/pages/LoginPage.tsx` | Console gate |
| `frontend/src/pages/AccountsPage.tsx` | Qoder login + pool |
| `frontend/src/pages/ProvidersPage.tsx` | Model catalog + context-window defaults |
| `frontend/src/components/layout/` | Shell / menu |
| `internal/accounts/` | SQLite account repository, scheduler, child lifecycle |
| `worker/src/daemon.mjs` | One-account Qoder runtime only |
| `worker/src/errors.mjs` | Error taxonomy |
| `internal/executor/chat.go` | Proxy → worker |

After UI edits: `cd frontend && npm run sync` so `internal/webui/static` stays in lockstep.
