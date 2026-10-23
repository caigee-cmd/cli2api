### English

- Non-lite Codex requests that include tools now preserve an explicit `parallel_tool_calls: false` from the caller instead of forcing `true` (aligned with CLIProxyAPI). Absent values still default to `true`.

### 中文

- 非 lite 且带 tools 的 Codex 请求会保留调用方显式传入的 `parallel_tool_calls: false`，不再强制改成 `true`（与 CLIProxyAPI 对齐）；未传时仍默认 `true`。
