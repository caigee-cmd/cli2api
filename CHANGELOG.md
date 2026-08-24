# Changelog

## Unreleased

- Add next-version-only managed updates through a Linux Unix Socket or authenticated macOS/Windows host updater
- Refresh the account console with compact controls and accessible GSAP enable-state motion
- Snapshot and verify SQLite before replacement, preserving the existing data volume
- Restore and re-pin the previous image plus SQLite snapshot when the new version fails health checks
- Track immutable SQLite migrations with SHA256 checksums
- Add native Windows PowerShell startup and current-user Scheduled Task installation
- Publish `linux/amd64` and `linux/arm64` container images from one release workflow
- Attach Linux, macOS, and Windows updater binaries for `amd64` and `arm64`
- Verify updater downloads with a SHA256 checksum manifest
- Version the host updater protocol and reject unknown incompatible versions
- Add a one-command workflow that calculates and publishes the next patch release

## 0.2.0 - 2026-08-23

- Replace the supervisor-based pool with a Go-owned SQLite account registry
- Run one isolated Node daemon and HOME per enabled Qoder account
- Add account CRUD, browser OAuth, PAT, native credential import/export, cooldown and failover
- Move deployment to one container with persistent `qoder-data`
- Redesign the HeroUI console with responsive light and dark themes

## 0.1.0 - 2026-08-22

- OpenAI-compatible streaming and non-streaming chat
- Tool calls and reasoning passthrough
- Hot Qoder WASM/auth context with pinned qodercli compatibility checks
- React console and loopback-only Docker Compose deployment
- Upstream usage support with token estimation fallback
- Redacted protocol notes and secret-scanning CI
