# AGENTS

Go + Node gateway for personal Qoder, WorkBuddy, Trae CN Work, and experimental Devin accounts, with OpenAI / Anthropic-compatible endpoints.

## Docs

Read this file first. Internal contract and planning docs carry YAML frontmatter
(`id / title / scope / status / read-when / summary / related / last-updated`);
use `read-when` to decide whether to open it.

| File | Read when | What belongs there |
|------|-----------|--------------------|
| `AGENTS.md` | always | Hard rules for agents. Short. |
| `docs/ARCHITECTURE_SUMMARY.md` | quick backend orientation | Public package and runtime map for contributors and AI. |
| `docs/ARCHITECTURE.md` (ignored) | detailed backend, protocol adapters, login, account routing, migrations, console IA, managed update | Local detailed contract; unavailable in a clean checkout. |
| `docs/DESIGN.md` | any console UI work | Public frontend design system: tokens, radii, type, HeroUI picks, copy. |
| `docs/REQUEST.md` (ignored) | detailed routing, failover, cooldown, session affinity, error taxonomy | Local request contract; use code/tests and the architecture summary in a clean checkout. |
| `docs/PLAN.md` (ignored) | milestone work | Local working checklist, not a public product contract. |
| `docs/PROVIDERS.md` (ignored) | adding or designing a provider | Local provider facts and extension notes. |
| `docs/PROVIDERS_TRAE_SOLO.md` (ignored) | Trae CN Work adapter work | Local provider survey and protocol notes. |
| `docs/DEVELOPMENT.md` | build / test / release | Public build, validation, and maintainer release workflow. |
| `changelog/unreleased/README.md` | user-facing PR notes / release notes | Per-PR bilingual fragments. `CHANGELOG.md` is the published archive |
| `docs/REFACTORING.md` | accepted package split | Tracked package-boundary baseline; A–D cleanup is recorded as completed. Do not re-run S00–S15. |
| `docs/capture-notes.md` (ignored) | protocol facts | Local redacted protocol facts |
| `docs/PRIVATE_DEPLOYMENT.md` (ignored) | host ops | Host ops runbook |

Public AI-facing docs are:

- `AGENTS.md` and `CONTRIBUTING.md` for hard rules and contribution workflow.
- `docs/ARCHITECTURE_SUMMARY.md` for the backend package and runtime map.
- `docs/DESIGN.md` for console UI work.
- `docs/DEVELOPMENT.md` for validation and release workflow.
- `docs/REFACTORING.md` for the accepted package-boundary record.

`docs/ARCHITECTURE.md`, `docs/REQUEST.md`, `docs/PLAN.md`, `docs/PROVIDERS*.md`, `docs/capture-notes.md`, and `docs/PRIVATE_DEPLOYMENT.md` are local-only details. They may not exist in a clean checkout and must not be assumed to be available. Do not add new `TODO.md`, `NOTES.md`, or extra plan files.
`docs/PROVIDERS_TRAE.md` is superseded; do not implement from it. User-facing
install stays in `README.md` (Chinese) / `README_EN.md` (English).
For a clean checkout, start with `AGENTS.md`, then use `docs/ARCHITECTURE_SUMMARY.md` for backend orientation, `docs/DESIGN.md` for UI work, and `docs/DEVELOPMENT.md` for validation and release. Use `docs/REFACTORING.md` as the package-boundary baseline;
verify behavior against current code, and keep implementation, local tests,
live-account acceptance, and release status separate.

## Do

- Pull latest `main` and merge it into the current branch before starting any feature work (skip only when already on up-to-date `main`)
- After console UI changes, run `cd frontend && npm run sync` so `internal/webui/static` matches `frontend/dist`. Do not commit a stale hashed JS/CSS pair.
- User-facing changes add one bilingual file in `changelog/unreleased/` (`### English` / `### 中文`). Do not put upcoming notes in `CHANGELOG.md` and do not add `## Unreleased` there. Do not freeze or unfreeze changelog sections by hand. Do not reuse a fragment filename until that tag's archive PR has merged.
- When `main` is checked out in another worktree, merge PRs with `gh api` / GitHub; do not `git checkout main` here.
- Keep architecture: auth / endpoint / executor / translate, plus `internal/store` (SQLite), `internal/control` (console facade: accounts, keys, settings, catalog, login, import), `internal/runtime` (Manager lifecycle), `internal/providers/qoder` (Qoder HOME/CLI/worker protocol and Adapter), `internal/gateway` (public protocol HTTP), `internal/console` (operator HTTP), `internal/server` (routes/middleware/webui), and `internal/app` (process assembly). `internal/api` is a test-only compatibility facade (`api.New` → `app.New`); do not add business there. Account entities stay in `accounts`; Pool/Item/RouteQuery/Classify and request Prepare live in `executor`. Display catalog cache lives in `control.Catalog`; catalog aggregation, identity filter, and settings decoration live in `control`, not `app`. Public `/v1/chat/completions`, `/v1/messages`, `/v1/responses`, and `/v1/models` live in `gateway`; console `/api/*` lives in `console`; `internal/server` registers both. Console HTTP decodes and maps errors; persist/apply for system settings and console-key rotation live in `control.System` / `control.KeyRotation`. Update job/maintenance lives in `internal/update.Coordinator`. SQLite lives in `internal/store`; process tables live in runtime. Runtime constructs the one Pool and injects it into executor. Qoder stays `child_process`; the registered Adapter omits Prober. Runtime catalog may use `adapter.Models`; quota/login/chat still use worker HTTP. `cmd/server` constructs `app.New`. `accounts` must not import `runtime` or `executor`. Executor Prepare must not take `*http.Request` or import store. Gateway, console, and server must not import store or runtime Manager. App/server/gateway/console must not import `internal/api`. Provider packages must not receive `http.ResponseWriter` or import executor taxonomy; OAuth loopback HTTP stays in `auth.ServeLoopback`. Adapter error classification and cooldown math live in `executor`; gateway only formats the result.
- Prefer direct HTTP/SSE to Qoder cloud APIs
- Pin qodercli / qoderclicn hooks in `worker/src/compat.mjs`; fail loudly on mismatch. Qoder CN is `provider=qoder` + `region=cn`, not a new family
- Reasoning levels are catalog-driven: map client values through `internal/providers/reasoning.go` (`none`/`low`/`medium`/`high`/`xhigh`/`max`), clamp anything the model does not allow back to an allowed level, and treat the console value as a default only (it never locks a call or caps a higher client value)
- Console UI: React + Tailwind v4 + **HeroUI only** for components
- Follow `docs/DESIGN.md` (taste v1 adapted for this console)
- Keep iterating Qoder login, usage, and account routing. Borrow scheduling ideas from [sub2api](https://github.com/Wei-Shaw/sub2api), not its commercial gateway
- Qoder multi-account = one worker process per HOME; do not share WASM context. WorkBuddy / Trae / Devin use in-process adapters, not one child process per account
- Schema changes go in a new numbered SQLite migration entry in `internal/store/migrations.go`. Never rewrite shipped SQL

## Don't

- Spawn a full `qodercli` agent per request
- Expose host ports publicly
- Commit raw auth blobs / tokens / host IPs / `docs/PRIVATE_DEPLOYMENT.md`
- Leave console `/api/*` or worker `/admin/*` unauthenticated
- Copy sub2api billing, Redis slots, multi-tenant API keys, or session-hash-for-profit
- Add a new component library, purple AI chrome, centered generic login cards, or emoji in UI copy
- Start Cursor / Anthropic only after the current Qoder milestone is explicitly confirmed complete; its detailed checklist is local-only in `docs/PLAN.md`. Qoder CN is that milestone (`provider=qoder` + `region=cn`); do not spawn a full `qoderclicn` per request
- Invent reasoning levels a model does not declare. Catalog effort wins: keep `onlyReasoning` models locked (DeepSeek is `high`), and do not give WorkBuddy a Trae-style Max switch or send a context-window switch on chat
- Change the SQL bytes of a shipped SQLite migration in `internal/store/migrations.go`. Tabs, spaces, and comments inside the raw string count. `gofmt` on the Go around it is fine; indenting the SQL is not. Existing databases panic on boot with `checksum mismatch`
