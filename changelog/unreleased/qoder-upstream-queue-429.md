### English

- Qoder upstream model-level queue rejections (business code 10605 / `RESOURCE_LIMIT`) are now reported as `429 rate_limited` with the upstream `retry_after` hint instead of the misleading `upstream_empty` ("context overflow / silent reject"). The gateway detects the queued rejection inside an HTTP 200 SSE frame, keeps the account healthy (no cooldown, no failover) because the queue applies to every account, and relays the retry hint on both streaming and buffered chat so clients back off correctly.

### 中文

- Qoder 上游的模型级排队拒绝（业务码 10605 / `RESOURCE_LIMIT`）现在会正确报告为 `429 rate_limited` 并透传上游的 `retry_after` 提示，而不再误报成 `upstream_empty`（"上下文溢出 / 静默拒绝"）。网关会从 HTTP 200 的 SSE 帧中识别这种排队拒绝；由于队列对所有账号一视同仁，网关不会因此冷却或切换账号，并在流式与缓冲式聊天中都保留重试提示，让客户端正确退避。
