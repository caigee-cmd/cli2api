# Development

last-updated: 2026-08-27

Repository rules, layering constraints, and validation checklists live in
[CONTRIBUTING.md](../CONTRIBUTING.md). This page covers the local build loop
and the maintainer release workflow.

## Requirements

Go `1.25.6+`, Node.js `20+`, npm, and Docker for container development.

## Validate

```bash
# Go API
go test ./...
go vet ./...

# Qoder worker
cd worker
npm test

# Console
cd ../frontend
npm ci
npm run build
npm run lint
```

After frontend changes, run `npm run sync` to update the static assets embedded by Go:

```bash
cd frontend
npm run sync
```

Build and start the container from source:

```bash
cd deploy
docker compose up -d --build
```

## Maintainer release

After `main` passes CI, publish the next patch release with one command:

```bash
gh workflow run release.yml --ref main
```

Write bilingual user-facing notes in `CHANGELOG.md` under `## Unreleased` before publishing. Each change needs a matching bullet in `### English` and `### 中文`; the workflow copies those notes into the GitHub Release and the console update page.

The workflow waits for the exact `main` commit to pass CI, calculates the next patch from the latest published stable release, creates an invisible draft release, builds six checksum-verified updater binaries, verifies the `linux/amd64` and `linux/arm64` image manifest, and only then publishes the GitHub Release and moves the stable image aliases. After publication it freezes the Unreleased notes under the new version heading. Do not create or push the version tag manually.

You can also use **Actions → Release → Run workflow**. If a pre-publication job fails, use **Re-run failed jobs** on the same run; the draft release remains invisible to application update checks.
