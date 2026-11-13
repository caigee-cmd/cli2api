### English

- Recognise client conversation ids from headers and body metadata aliases (`X-Conversation-Id`, `X-Session-Id`, `X-Thread-Id`, `conversation_id`, `conversationId`, `metadata.*`, ...), so chat turns stay on one account and keep the upstream prompt cache warm instead of rotating through the pool.
- Add opt-in `CLI2API_MSGSTAT_SHARDS` to bound one-shot Messages calls to a small, stable set of accounts per (API key, model, system prefix, tools), matching `WB_MSGSTAT_SHARDS` in workbuddy2api-hub.
- Add opt-in `CLI2API_AFFINITY_DEBUG` diagnostics that separate route-level session affinity from real upstream cache-token hits, including prompt/cache tokens and hit rate.
- Rectify historical Anthropic `thinking` / `redacted_thinking` blocks and stale signatures on `/v1/messages` (cc-switch-style minimal cleanup) so OpenAI-compatible upstreams no longer receive invalid signed blocks. Disable with `CLI2API_THINKING_RECTIFIER=off`.
- Skip WAF fingerprint regexes for request bodies with no known trigger, avoiding unnecessary per-request CPU work on large histories and keeping cached prefixes stable.

### 中文

- 支持从请求头和 body 元数据别名识别会话 ID（`X-Conversation-Id`、`X-Session-Id`、`X-Thread-Id`、`conversation_id`、`conversationId`、`metadata.*` 等），让同一对话固定在同一账号上，避免轮转导致上游 prompt 缓存失效。
- 新增可选 `CLI2API_MSGSTAT_SHARDS`，按（API Key、模型、system 前缀、tools）把单发 Messages 请求收敛到少量固定账号，对齐 workbuddy2api-hub 的 `WB_MSGSTAT_SHARDS`。
- 新增可选 `CLI2API_AFFINITY_DEBUG` 诊断日志，把「路由级会话亲和」与「上游真实缓存 token 命中」分开记录，包含 prompt/cache tokens 与命中率。
- `/v1/messages` 入站按 cc-switch 思路最小清理历史 `thinking` / `redacted_thinking` 块和失效签名，避免把无效签名转发给 OpenAI 兼容上游；可用 `CLI2API_THINKING_RECTIFIER=off` 关闭。
- 对不含任何已知指纹的请求体跳过 WAF 正则清洗，减少长历史请求的 CPU 开销，并避免清洗改动缓存前缀。
