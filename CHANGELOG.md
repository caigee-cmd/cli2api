# Changelog

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
