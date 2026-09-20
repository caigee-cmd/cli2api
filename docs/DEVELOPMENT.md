---
id: cli2api-development
title: Development
scope: [build, validate, release]
status: canonical
read-when: 本地构建 / 跑测试 / 发布维护者版本时
summary: 本地构建与校验命令，以及维护者发布流程（workflow_dispatch、unreleased fragments、归档 PR）。
related: [CONTRIBUTING.md, docs/ARCHITECTURE.md]
last-updated: 2026-09-19
---

# Development

Repository rules, layering constraints, and validation checklists live in
[CONTRIBUTING.md](../CONTRIBUTING.md). This page covers the local build loop
and the maintainer release workflow.

## Requirements

Go `1.25.6+`, Node.js `22+`, npm, and Docker for container development.

## Validate

```bash
# Go API
go test ./internal/app -run 'TestImportConstraints|TestDutyBoundaries' -count=1
go test ./...
go test -race ./...
go vet ./...

# Qoder child runtime
(cd worker && npm ci && npm test)

# Console
(cd frontend && npm ci && npm run build && npm run lint)
git diff --check
```

After frontend changes, run `npm run sync` to update the static assets embedded by Go:

```bash
cd frontend
npm run sync
```

`vite build` writes `frontend/dist`. The Go binary embeds `internal/webui/static`. Shipping a console change without `sync` leaves the previous hashed bundle in the image. Commit the new `index-*.js` / `index-*.css` and `index.html` in the same change; Git will show the old hashed files as deletions.

Build and start the container from source:

```bash
cd deploy
docker compose up -d --build
```

## Maintainer release

Do not create or push version tags by hand. The tag, GitHub Release, updater assets, and GHCR image aliases all come from one serialized `workflow_dispatch` on `main`.

### Before you run it

1. `main` is the commit you want to ship. The workflow waits for CI on that exact SHA.
2. `changelog/unreleased/` has bilingual fragment files for every user-facing change on `main` since the last published tag. The workflow concatenates those files into the GitHub Release and the console System page. `validate` allows an empty unreleased directory; `extract-for-release` fails if it is empty and the new version heading does not already exist.
3. Do not archive fragments or edit `CHANGELOG.md` yourself before the run. The workflow reads `changelog/unreleased/` first; archive only after the tag is public, through the pull request the workflow opens.
4. Do not ship a change that edits the SQL bytes of an already-applied SQLite migration. Existing databases panic on boot with `checksum mismatch`, and the host updater rolls back. Append a new numbered entry in `internal/store/migrations.go`; details are in `docs/ARCHITECTURE.md`.

```bash
gh workflow run release.yml --ref main
```

You can also use **Actions → Release → Run workflow**.

The workflow calculates the next patch from the latest published stable release, creates an invisible draft, builds six checksum-verified updater binaries plus `cli2api-updater_checksums.txt`, publishes `linux/amd64` + `linux/arm64` images, then makes the GitHub Release latest and moves `latest` / series aliases. Console update checks ignore drafts, so a failed pre-publication run stays invisible.

### After it publishes

The `changelog` job opens a `docs: archive changelog for v0.x.y` pull request. It copies the published GitHub Release body under `## 0.x.y - YYYY-MM-DD` and deletes only the `changelog/unreleased/` files that existed at the tagged commit. Fragments merged after the tag stay unpublished.

Merge that PR. Do not re-run `release.yml` to archive notes — a second run would mint the next patch. Treat a missing archive PR as the thing to fix, not a red `publish` / `aliases` job.

If required checks do not start on the bot PR (the default Actions token cannot retrigger workflows), push an empty commit to that branch from a local checkout.

If this checkout cannot switch to `main` because another worktree already has it, merge with the GitHub API instead of `git checkout main`:

```bash
gh api -X PUT repos/caigee-cmd/cli2api/pulls/<n>/merge -f merge_method=merge
```

Expected artifacts for a published tag:

| Kind | Names |
|------|--------|
| GitHub Release assets (7) | six `cli2api-updater_{os}_{arch}` binaries and `cli2api-updater_checksums.txt` |
| GHCR (`ghcr.io/caigee-cmd/cli2api`) | `v0.x.y`, `0.x.y`, series (`0.2`), `latest`, all the same multi-arch digest |

If a **pre-publication** job fails (`prepare` / `assets` / `draft` / `image` / `promote`), use **Re-run failed jobs** on the same run. The draft remains unpublished. Do not create the tag locally while that draft exists.
