# DESIGN

last-updated: 2026-08-23

Canonical design and product notes for agents. Plans live in `docs/PLAN.md`. Hard rules live in `AGENTS.md`.

Reference backend: [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api).  
We keep borrowing **account routing, failover, cooldown, in-flight caps**.  
We do **not** copy billing, Redis concurrency, multi-tenant keys, or commercial sticky sessions.

Console taste: [design-taste-frontend-v1](https://github.com) adapted for a self-hosted ops console. Components **must** be HeroUI.

## Runtime

```text
Client (OpenAI SDK / Codex / CherryStudio)
  -> qoder-api-proxy (:3010)          # Go: auth, translate, pool, console
    -> qoder-auth-worker (:3020)      # Node: hot QoderContext
      -> https://api1.qoder.sh/.../agent_chat_generation?Encode=1
```

Worker pins `@qoder-ai/qodercli@1.1.27`. Needle mismatch exits loudly.

## Two logins

They are not the same password.

| Gate | Secret | Where | Unlocks |
|------|--------|-------|---------|
| Console | `PROXY_API_KEY` | `/login` | Overview, accounts, models, API test |
| Qoder | device-flow / PAT | `/accounts` per worker | Upstream chat for that HOME |

`/health` stays open. `/api/*`, `/v1`, worker `/admin/*` and chat require the console key when it is set.

Placeholder keys (`""`, `change-me`, `dev-key`) fail fast unless `ALLOW_INSECURE_API_KEY=1`.

## Account routing

Qoder WASM / AuthManager is process-global. One HOME = one worker process.

Default compose: one worker container, `QODER_HOMES=acc1=/root,acc2=/home/acc2`, supervisor inside.  
Separate containers: `QODER_WORKER_URLS` + `QODER_ACCOUNT_IDS`.

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
| `/providers` | Models | Catalog from the signed-in worker |
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
| `frontend/src/components/layout/` | Shell / menu |
| `worker/src/supervisor.mjs` | Process isolation + failover |
| `worker/src/errors.mjs` | Error taxonomy |
| `internal/accounts/` | Go pool + classify |
| `internal/executor/chat.go` | Proxy → worker |

After UI edits: `cd frontend && npm run sync` so `internal/webui/static` stays in lockstep.
