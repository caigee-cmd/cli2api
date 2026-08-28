# Development

last-updated: 2026-08-28

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

Do not create or push version tags by hand. The tag, GitHub Release, updater assets, and GHCR image aliases all come from one serialized `workflow_dispatch` on `main`.

### Before you run it

1. `main` is the commit you want to ship. The workflow waits for CI on that exact SHA.
2. `CHANGELOG.md` `## Unreleased` has matching `### English` and `### 中文` bullets for every user-facing change on `main` since the last published tag. The workflow copies that section into the GitHub Release and the console System page. `validate` allows an empty Unreleased section; `extract-for-release` fails if Unreleased is empty and the new version heading does not already exist.
3. Do not freeze Unreleased yourself before the run. The workflow reads Unreleased first; freeze only after the tag is public.

```bash
gh workflow run release.yml --ref main
```

You can also use **Actions → Release → Run workflow**.

The workflow calculates the next patch from the latest published stable release, creates an invisible draft, builds six checksum-verified updater binaries plus `cli2api-updater_checksums.txt`, publishes `linux/amd64` + `linux/arm64` images, then makes the GitHub Release latest and moves `latest` / series aliases. Console update checks ignore drafts, so a failed pre-publication run stays invisible.

### After it publishes

The `changelog` job tries to freeze Unreleased under the new version heading and push straight to `main`. That push is rejected: `main` requires a pull request and status checks. Treat a red Release run as expected when `publish` and `aliases` already succeeded.

Freeze by PR, not by re-running the release workflow:

1. Branch from current `main` (it may already have commits after the tag).
2. Move only the bullets that shipped in that tag under `## 0.x.y - YYYY-MM-DD`. Leave later `main` work in `## Unreleased`.
3. Open a `docs: freeze changelog for v0.x.y` PR. If `CHANGELOG.md` conflicts, keep post-tag features in Unreleased.
4. Merge the PR. Do not run `release.yml` again to fix the freeze — a second run would mint the next patch.

Expected artifacts for a published tag:

| Kind | Names |
|------|--------|
| GitHub Release assets (7) | six `cli2api-updater_{os}_{arch}` binaries and `cli2api-updater_checksums.txt` |
| GHCR (`ghcr.io/caigee-cmd/cli2api`) | `v0.x.y`, `0.x.y`, series (`0.2`), `latest`, all the same multi-arch digest |

If a **pre-publication** job fails (`prepare` / `assets` / `draft` / `image` / `promote`), use **Re-run failed jobs** on the same run. The draft remains unpublished. Do not create the tag locally while that draft exists.
