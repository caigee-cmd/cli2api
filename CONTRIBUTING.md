# Contributing

CLI2API keeps the Qoder execution path stable while supporting provider-specific
adapters for Qoder, WorkBuddy, Trae, and experimental Devin. Keep new provider work behind the shared
account, routing, and protocol contracts. Read [AGENTS.md](AGENTS.md) for hard
rules and [REFACTORING.md](docs/REFACTORING.md) for accepted package boundaries
and the separate, still-pending cleanup. Preserve existing uncommitted work;
before feature work, fetch and merge the latest `origin/main` into your branch.

## Setup

Requirements: Go from `go.mod`, Node 22+, and npm.

```bash
go mod download
(cd worker && npm ci)
(cd frontend && npm ci)
```

## Validate

```bash
go test ./internal/app -run 'TestImportConstraints|TestDutyBoundaries' -count=1
go test ./...
go test -race ./...
go vet ./...
(cd worker && npm test)
(cd frontend && npm run build && npm run lint)
git diff --check
```

After frontend changes:

```bash
cd frontend && npm run sync
```

`npm run build` only writes `frontend/dist`. Go embeds `internal/webui/static`, so a console change is not in the binary until `sync` runs. Commit the new hashed JS/CSS and `index.html` together; do not leave an old `index-*.js` next to a new `index.html`.

If `main` is checked out in another worktree, merge PRs through GitHub or `gh api`;
do not check out `main` in the current worktree.

## Documentation and releases

Keep README text and diagrams aligned in Chinese and English. Asset maintenance
notes live in [docs/assets/README.md](docs/assets/README.md). Describe implemented
features separately from pending live-account, managed-update, and release acceptance.

For an end-to-end run, use the Docker Compose flow in `deploy/README.md`.

User-facing changes should add one bilingual Markdown file under
`changelog/unreleased/` (`### English` and `### 中文`, matching bullet counts).
Skip that directory for tests, refactors, CI, and docs-only work. File naming
and shape are in `changelog/unreleased/README.md`. Do not edit `CHANGELOG.md`
for upcoming notes; it is the published archive. The release workflow concatenates
the unreleased files into the GitHub Release body, then opens a pull request to
archive them. Do not create version tags by hand. The workflow defaults to the
next patch; choose `minor` or `major` when the published behavior warrants a
series bump.

## Rules

- Keep the Go layers: auth / endpoint / executor / translate, plus store / control / runtime / gateway / console / server / app. `internal/api` is a test-only facade over `app.New`; do not add business there.
- Keep one isolated runtime per enabled account: Qoder uses one HOME and Node daemon;
  in-process providers use their adapter and must not spawn a child daemon
- Keep qodercli compatibility checks in `worker/src/compat.mjs`
- Preserve the proven WASM encode and HTTP/SSE request path
- Use HeroUI for console components
- Add tests for account, routing, API, or translation behavior changes
- Treat shipped SQLite migration SQL as immutable. Append a new numbered entry in `internal/store/migrations.go`; pin checksums in `internal/store` tests.
- Do not commit `.env`, `.qoder`, auth blobs, tokens, raw captures, host IPs, or
  `docs/PRIVATE_DEPLOYMENT.md`

Hard rules live in `AGENTS.md`. Detailed milestone, provider, protocol-capture, and
deployment notes stay local and gitignored (`docs/PLAN.md`, `docs/ARCHITECTURE.md`,
`docs/REQUEST.md`, `docs/PROVIDERS*.md`, …). Public contributor guidance lives in
`docs/DESIGN.md`, `docs/DEVELOPMENT.md`, and `docs/ARCHITECTURE_SUMMARY.md`.
`docs/REFACTORING.md` remains the tracked package-boundary record.
