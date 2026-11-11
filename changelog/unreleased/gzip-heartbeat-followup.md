### English

- Gzip request bodies that inflate past the decompressed size limit are now rejected with a clear "request body too large" error (413) instead of being silently truncated and then failing as a JSON syntax error.
- Body-decoding failures on `POST /v1/messages` (unsupported encoding, corrupt gzip, over-limit inflate) now use the Anthropic error envelope this endpoint already returns for other request errors, not the OpenAI shape.
- Streaming chat heartbeats still send `X-CLI2API-Account`, `X-Qoder-Account`, and `X-CLI2API-Provider` when the SSE headers are committed early: those values are taken from the account already picked while waiting for the upstream response head. A client disconnect during that heartbeat wait is recorded as canceled, not as a normal stream open.

### 中文

- gzip 请求体解压超过上限时现在会直接拒绝，并返回明确的「request body too large」（413），不再静默截断后再被当成 JSON 语法错误。
- `POST /v1/messages` 的请求体解码失败（不支持的编码、损坏的 gzip、解压超限）改回该接口原有的 Anthropic 错误格式，而不再使用 OpenAI 形状。
- 流式对话在提前提交 SSE 响应头时仍会带上 `X-CLI2API-Account`、`X-Qoder-Account` 与 `X-CLI2API-Provider`：这些值来自等待上游响应头期间已经选出的账号。客户端在这段心跳等待中断开时记为取消，而不再记成一次正常开流。
