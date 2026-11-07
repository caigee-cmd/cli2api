### English

- Streaming chat now heartbeats while it waits for the upstream response head: after `CLI2API_STREAM_HEARTBEAT_AFTER_MS` (default 20s) the gateway commits SSE headers and emits `: keepalive` comment frames every `CLI2API_STREAM_HEARTBEAT_INTERVAL_MS` (default 10s) until the real stream starts. A reverse proxy with a fixed response budget (Cloudflare's 120s Proxy Read Timeout, which is Enterprise-only to raise) no longer aborts a healthy turn whose prompt is slow to upload or whose prefill runs long. Fast failures keep returning their real HTTP status, because the headers are only committed once the attempt proves slow; a failure after that point is reported as an in-stream error event instead.

### 中文

- 流式对话在等待上游响应头期间开始发心跳：超过 `CLI2API_STREAM_HEARTBEAT_AFTER_MS`（默认 20s）后先提交 SSE 响应头，并按 `CLI2API_STREAM_HEARTBEAT_INTERVAL_MS`（默认 10s）持续发送 `: keepalive` 注释帧，直到真实流开始。这样带固定响应预算的反向代理（Cloudflare 的 120s Proxy Read Timeout，非 Enterprise 不可调）就不会因为上传慢或 prefill 久而掐断一个正常请求。快速失败仍然返回真实 HTTP 状态码——只有确认这次尝试确实慢的时候才提前提交响应头；提交之后的失败改为流内 error 事件。
