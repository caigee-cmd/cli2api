### English

- Enforce a tested Content-Security-Policy on every response while keeping console scripts, styles, fonts, and images working.
- Gzip console JSON responses and `/v1/models`; SSE and chat streams stay uncompressed.
- Rate-limit invalid API keys per client, return `429` with `Retry-After`, and clear the counter after a successful authentication.
- Enlarge the request-log queue, wait briefly before dropping, rate-limit the drop log, and expose `log_dropped` / `log_queue` in `/health`.

### 中文

- 所有响应启用经实测的 Content-Security-Policy，同时保持控制台脚本、样式、字体与图片正常加载。
- 控制台 JSON 与 `/v1/models` 启用 gzip；SSE 与聊天流保持不压缩。
- 按客户端限制无效 API Key 重试，超限返回 `429` 与 `Retry-After`，鉴权成功即清零。
- 扩大请求日志队列，满队列短暂等待后再丢弃，丢弃日志限频，并在 `/health` 暴露 `log_dropped` / `log_queue`。
