# AGENTS

Go + Node service that turns a local Qoder CLI login into an OpenAI-compatible API.

Keep writing-code docs in these files only:

| File | What belongs there |
|------|--------------------|
| `AGENTS.md` | Hard rules for agents. Short. |
| `docs/DESIGN.md` | Architecture, login, routing, console, design system. |
| `docs/PLAN.md` | Current milestone checklist. |
| `docs/PROVIDERS.md` | Future account-provider design. Not a current milestone. |

Do not add new `TODO.md`, `NOTES.md`, or extra plan files. The only extra design doc is `docs/PROVIDERS.md` (WorkBuddy / Qoder CN / future account types). User-facing install stays in `README.md` (Chinese) / `README_EN.md` (English). Local protocol facts stay in ignored `docs/capture-notes.md`. Host ops stay gitignored in `docs/PRIVATE_DEPLOYMENT.md`.

## Do

- Pull latest `main` and merge it into the current branch before starting any feature work (skip only when already on up-to-date `main`)
- After console UI changes, run `cd frontend && npm run sync` so `internal/webui/static` matches `frontend/dist`. Do not commit a stale hashed JS/CSS pair.
- When `main` is checked out in another worktree, merge PRs with `gh api` / GitHub; do not `git checkout main` here.
- Keep architecture: auth / endpoint / executor / translate / api
- Prefer direct HTTP/SSE to Qoder cloud APIs
- Pin qodercli / qoderclicn hooks in `worker/src/compat.mjs`; fail loudly on mismatch. Qoder CN is `provider=qoder` + `region=cn`, not a new family
- Console UI: React + Tailwind v4 + **HeroUI only** for components
- Follow `docs/DESIGN.md` (taste v1 adapted for this console)
- Keep iterating Qoder login, usage, and account routing. Borrow scheduling ideas from [sub2api](https://github.com/Wei-Shaw/sub2api), not its commercial gateway
- Multi-account = one worker process per Qoder HOME; do not share WASM context

## Don't

- Spawn a full `qodercli` agent per request
- Expose host ports publicly
- Commit raw auth blobs / tokens / host IPs / `docs/PRIVATE_DEPLOYMENT.md`
- Leave console `/api/*` or worker `/admin/*` unauthenticated
- Copy sub2api billing, Redis slots, multi-tenant API keys, or session-hash-for-profit
- Add a new component library, purple AI chrome, centered generic login cards, or emoji in UI copy
- Start Cursor / Anthropic until the current Qoder milestone in `docs/PLAN.md` is done. Qoder CN is that milestone (`provider=qoder` + `region=cn`); do not spawn a full `qoderclicn` per request
