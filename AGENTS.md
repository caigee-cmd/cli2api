# AGENTS

Go + Node service that turns a local Qoder CLI login into an OpenAI-compatible API.

Keep writing-code docs in three files only:

| File | What belongs there |
|------|--------------------|
| `AGENTS.md` | Hard rules for agents. Short. |
| `docs/DESIGN.md` | Architecture, login, routing, console, design system. |
| `docs/PLAN.md` | Current milestone checklist. |

Do not add new `TODO.md`, `NOTES.md`, or extra plan files. User-facing install stays in `README.md` / `README_ZH.md`. Protocol facts stay in `docs/capture-notes.md`. Host ops stay gitignored in `docs/PRIVATE_DEPLOYMENT.md`.

## Do

- Keep architecture: auth / endpoint / executor / translate / api
- Prefer direct HTTP/SSE to Qoder cloud APIs
- Pin qodercli hooks in `worker/src/compat.mjs`; fail loudly on mismatch
- Console UI: React + Tailwind v4 + **HeroUI only** for components
- Follow `docs/DESIGN.md` (taste v1 adapted for this console)
- Keep iterating Qoder login, usage, and account routing. Borrow scheduling ideas from [sub2api](https://github.com/Wei-Shaw/sub2api), not its commercial gateway
- Multi-account = one worker process per Qoder HOME; do not share WASM context

## Don't

- Spawn a full `qodercli` agent per request
- Expose host ports publicly
- Commit raw auth blobs / tokens / host IPs / `docs/PRIVATE_DEPLOYMENT.md`
- Leave console `/api/*` or worker `/admin/*` unauthenticated when `PROXY_API_KEY` is set
- Copy sub2api billing, Redis slots, multi-tenant API keys, or session-hash-for-profit
- Add a new component library, purple AI chrome, centered generic login cards, or emoji in UI copy
- Start Cursor / Anthropic until the current Qoder milestone in `docs/PLAN.md` is done
