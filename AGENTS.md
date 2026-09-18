# AGENTS

Go + Node service that turns a local Qoder CLI login into an OpenAI-compatible API.

## Docs

Read this file first. Every other doc carries YAML frontmatter
(`id / title / scope / status / read-when / summary / related / last-updated`);
use `read-when` to decide whether to open it.

| File | Read when | What belongs there |
|------|-----------|--------------------|
| `AGENTS.md` | always | Hard rules for agents. Short. |
| `docs/ARCHITECTURE.md` | backend, protocol adapters, login, account routing, migrations, console IA, managed update | Architecture, runtime, login, routing contract, console IA |
| `docs/DESIGN.md` | any console UI work | Frontend design system: tokens, radii, type, HeroUI picks, copy |
| `docs/REQUEST.md` | routing, failover, cooldown, session affinity, error taxonomy | Per-request pick / failover / cooldown contract |
| `docs/PLAN.md` | before starting a milestone | Current milestone checklist |
| `docs/PROVIDERS.md` | adding / designing a provider | Future account-provider design. Not a current milestone. |
| `docs/PROVIDERS_TRAE_SOLO.md` | Trae CN Solo work | Trae CN Solo in-process adapter survey. Not a current milestone. |
| `docs/DEVELOPMENT.md` | build / test / release | Local build loop and maintainer release workflow |
| `docs/REFACTORING.md` | behavior-preserving backend split | Accepted S00–S15 package split and remaining allowlist. Exception to “no extra plan files”. |
| `docs/capture-notes.md` (ignored) | protocol facts | Local redacted protocol facts |
| `docs/PRIVATE_DEPLOYMENT.md` (ignored) | host ops | Host ops runbook |

Keep these files only. Do not add new `TODO.md`, `NOTES.md`, or extra plan files. `docs/REFACTORING.md` is the one approved exception; do not add more plan files beside it.
`docs/PROVIDERS_TRAE.md` is superseded; do not implement from it. User-facing
install stays in `README.md` (Chinese) / `README_EN.md` (English).

## Do

- Pull latest `main` and merge it into the current branch before starting any feature work (skip only when already on up-to-date `main`)
- After console UI changes, run `cd frontend && npm run sync` so `internal/webui/static` matches `frontend/dist`. Do not commit a stale hashed JS/CSS pair.
- When `main` is checked out in another worktree, merge PRs with `gh api` / GitHub; do not `git checkout main` here.
- Keep architecture: auth / endpoint / executor / translate, plus `internal/store` (SQLite), `internal/control` (console facade), `internal/runtime` (Manager lifecycle), `internal/providers/qoder` (Qoder HOME/CLI/worker protocol and Adapter), `internal/gateway` (public protocol HTTP), `internal/console` (operator HTTP), `internal/server` (routes/middleware/webui), and `internal/app` (process assembly). `internal/api` is a test-only compatibility facade (`api.New` → `app.New`); do not add business there. Account entities stay in `accounts`; Pool/Item/RouteQuery/Classify and request Prepare live in `executor`. Display catalog cache lives in `control.Catalog`. Public `/v1/chat/completions`, `/v1/messages`, `/v1/responses`, and `/v1/models` live in `gateway`; console `/api/*` lives in `console`; `internal/server` registers both. Update job/maintenance lives in `internal/update.Coordinator`. SQLite lives in `internal/store`; process tables live in runtime. Runtime constructs the one Pool and injects it into executor. Qoder stays `child_process`; the registered Adapter omits Prober. Runtime catalog may use `adapter.Models`; quota/login/chat still use worker HTTP. `cmd/server` constructs `app.New`. `accounts` must not import `runtime` or `executor`. Executor Prepare must not take `*http.Request` or import store. Gateway, console, and server must not import store or runtime Manager. App/server/gateway/console must not import `internal/api`. Provider packages must not receive `http.ResponseWriter` or import executor taxonomy.
- Prefer direct HTTP/SSE to Qoder cloud APIs
- Pin qodercli / qoderclicn hooks in `worker/src/compat.mjs`; fail loudly on mismatch. Qoder CN is `provider=qoder` + `region=cn`, not a new family
- Reasoning levels are catalog-driven: map client values through `internal/providers/reasoning.go` (`none`/`low`/`medium`/`high`/`xhigh`/`max`), clamp anything the model does not allow back to an allowed level, and treat the console value as a default only (it never locks a call or caps a higher client value)
- Console UI: React + Tailwind v4 + **HeroUI only** for components
- Follow `docs/DESIGN.md` (taste v1 adapted for this console)
- Keep iterating Qoder login, usage, and account routing. Borrow scheduling ideas from [sub2api](https://github.com/Wei-Shaw/sub2api), not its commercial gateway
- Multi-account = one worker process per Qoder HOME; do not share WASM context
- Schema changes go in a new numbered SQLite migration. Never rewrite a shipped file

## Don't

- Spawn a full `qodercli` agent per request
- Expose host ports publicly
- Commit raw auth blobs / tokens / host IPs / `docs/PRIVATE_DEPLOYMENT.md`
- Leave console `/api/*` or worker `/admin/*` unauthenticated
- Copy sub2api billing, Redis slots, multi-tenant API keys, or session-hash-for-profit
- Add a new component library, purple AI chrome, centered generic login cards, or emoji in UI copy
- Start Cursor / Anthropic until the current Qoder milestone in `docs/PLAN.md` is done. Qoder CN is that milestone (`provider=qoder` + `region=cn`); do not spawn a full `qoderclicn` per request
- Invent reasoning levels a model does not declare. Catalog effort wins: keep `onlyReasoning` models locked (DeepSeek is `high`), and do not give WorkBuddy a Trae-style Max switch or send a context-window switch on chat
- Change the SQL bytes of a shipped SQLite migration in `internal/store/migrations.go`. Tabs, spaces, and comments inside the raw string count. `gofmt` on the Go around it is fine; indenting the SQL is not. Existing databases panic on boot with `checksum mismatch`
