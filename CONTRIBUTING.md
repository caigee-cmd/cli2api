# Contributing

This repo is Qoder-first. Cursor and other CLIs are later providers.

## Dev loop

```bash
cp .env.example .env   # set PROXY_API_KEY
cd worker && npm test
go test ./...
cd frontend && npm install && npm run sync
```

Local run without Docker:

```bash
cd worker
PROXY_API_KEY=dev-key ALLOW_INSECURE_API_KEY=1 npm start

# another terminal
PROXY_API_KEY=dev-key ALLOW_INSECURE_API_KEY=1 \
QODER_WORKER_URL=http://127.0.0.1:3020 \
QODER_WORKER_API_KEY=dev-key \
go run ./cmd/server
```

## Rules

See `AGENTS.md`. Design and login/routing notes: `docs/DESIGN.md`. Current work: `docs/PLAN.md`.

- Keep architecture: auth / endpoint / executor / translate / api
- Pin qodercli hooks in `worker/src/compat.mjs`; fail loudly on mismatch
- Multi-account = one worker process per Qoder HOME
- Console UI uses HeroUI; do not add another component library
- Do not commit `.env`, `docs/PRIVATE_DEPLOYMENT.md`, raw captures, or tokens
