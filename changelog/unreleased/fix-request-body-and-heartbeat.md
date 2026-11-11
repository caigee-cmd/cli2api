### English

- Fix: a gzip request body that inflates past the decompressed size limit is rejected with `request body too large` (413) instead of being silently truncated into a JSON syntax error. Body-decoding failures on `POST /v1/messages` keep the Anthropic error envelope. A streaming heartbeat that commits its SSE headers early still sends `X-CLI2API-Account`, `X-Qoder-Account`, and `X-CLI2API-Provider`, and a client disconnect during that wait is recorded as canceled rather than a normal stream open.

### 中文

- 修复：gzip 请求体解压超过上限时直接拒绝并返回「request body too large」（413），不再静默截断后被当成 JSON 语法错误。`POST /v1/messages` 的请求体解码失败保持 Anthropic 错误格式。流式心跳在提前提交 SSE 响应头时仍会带上 `X-CLI2API-Account`、`X-Qoder-Account` 与 `X-CLI2API-Provider`；客户端在这段等待中断开时记为取消，不再记成一次正常开流。
