# Unreleased changelog fragments

User-facing notes for the next GitHub Release and the console update page.

Do not edit `CHANGELOG.md` for upcoming work, and do not add `## Unreleased` there.
`CHANGELOG.md` is the published archive. Upcoming notes live in this directory as
one bilingual Markdown file per pull request.

## When to add a file

Add a file when the PR changes something a console or API user can notice:
behavior, public endpoints, setup, or documented limits.

Skip this directory for tests, refactors, CI, and docs-only work that does not
change user-facing behavior.

## File name

Use lowercase ASCII kebab-case: `check-in-scheduler.md`.
`scripts/release-notes.py validate` rejects spaces and uppercase names.
Do not name the file after a version. The release workflow assigns the version.

## File shape

Start with `### English` and `### 中文`. Matching bullet counts are required.

```markdown
### English

- Keep session affinity from pinning a later model onto an empty-catalog account.

### 中文

- 会话粘性不会再把后续模型钉到空 catalog 账号上。
```

One pull request, one file. Multiple bullets in that file are fine.
Do not use `##` headings inside a fragment. Do not reuse a filename that
already shipped until the archive pull request for that tag has merged.

## Release

`workflow_dispatch` on `main` concatenates every `*.md` file in this directory
(except this README) into the GitHub Release body. After the tag is public, the
workflow opens a `docs: archive changelog for v0.x.y` pull request: it inserts
those notes under the new version heading in `CHANGELOG.md` and deletes only the
files that existed at the tagged commit. Fragments merged after the tag stay here
for the next release.

Do not freeze or unfreeze `CHANGELOG.md` by hand, and do not re-run `release.yml`
to archive notes. The old `release-notes.py freeze` command is gone; use
`consume` only from the release workflow.
