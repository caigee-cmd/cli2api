### English

- Codex credentials that enable `use_siwc` now keep both the outer ChatGPT OAuth and SIWC partitions. Requests whose User-Agent identifies an official Codex client (`codex-desktop`, `Codex Desktop`, `codex-rs`, `codex_cli_rs`, `codex-tui`, `codex_exec`) ignore `use_siwc`, use the outer token, and fail closed with 401 when the outer `access_token` is missing; refresh on that path only updates the outer partition.

### 中文

- 启用 `use_siwc` 的 Codex 凭据会同时保留 outer ChatGPT OAuth 与 SIWC 分区。入站 User-Agent 识别为官方 Codex 客户端（`codex-desktop` / `Codex Desktop` / `codex-rs` / `codex_cli_rs` / `codex-tui` / `codex_exec`）时忽略 `use_siwc`、改用 outer token；缺少 outer `access_token` 则直接 401，且该路径下的刷新只更新 outer。
