# Contributing

CLI2API is Qoder-first. Keep changes focused and do not add other providers until the
current Qoder milestone in `docs/PLAN.md` is complete.

## Setup

Requirements: Go from `go.mod`, Node 22+, and npm.

```bash
go mod download
cd worker && npm install
cd ../frontend && npm ci
```

## Validate

```bash
go test ./...
go vet ./...
cd worker && npm test
cd ../frontend && npm run build && npm run lint
```

After frontend changes:

```bash
cd frontend && npm run sync
```

For an end-to-end run, use the Docker Compose flow in `deploy/README.md`.

User-facing changes should add matching bullets to `CHANGELOG.md` under
`## Unreleased` in both `### English` and `### 中文`. The release workflow
copies those notes into the GitHub Release body.

## Rules

- Keep the Go layers: auth / endpoint / executor / translate / api
- Keep one Qoder HOME and one Node daemon per enabled account
- Keep qodercli compatibility checks in `worker/src/compat.mjs`
- Preserve the proven WASM encode and HTTP/SSE request path
- Use HeroUI for console components
- Add tests for account, routing, API, or translation behavior changes
- Do not commit `.env`, `.qoder`, auth blobs, tokens, raw captures, host IPs, or
  `docs/PRIVATE_DEPLOYMENT.md`

Architecture and UI decisions live in `docs/DESIGN.md`. Work in progress lives in
`docs/PLAN.md`.
