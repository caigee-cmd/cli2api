### English

- Keep aggregate logical-model sessions bound to a member account instead of resetting to the general pool on every turn.
- Bind a streaming request as soon as the upstream accepts the response headers/first event, not after the body ends; a follow-up turn can then reuse the same account while the first stream is still running.
- Always bind, cool down, and fail over by pool account ID. Worker response labels are never used as routing keys.
- Invalidate a session binding on failover, including sticky escapes.
- Canonicalize provider cache token fields so reported cache usage is stable across provider protocols.

### 中文

- 聚合逻辑模型的会话持续绑定到成员账号，不再每轮重置为通用路由池。
- 流式请求在上游接受响应头/首个成功事件时就完成绑定，而不是等整个响应体结束；第一轮流式请求尚未结束时，下一轮也能复用同一账号。
- 绑定、冷却和故障转移全部使用 pool account ID；worker 回写的响应标签不会作为路由键。
- 发生 failover 时失效会话绑定，包括 sticky-escape 场景。
- 统一各 provider 的缓存 token 字段口径，让上报的缓存用量在不同协议下保持稳定。
