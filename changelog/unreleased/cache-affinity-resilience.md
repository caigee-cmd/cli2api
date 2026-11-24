### English

- Retry a transient upstream failure once on the same account before failing over: a stream that dies before its first event or returns an empty body usually recovers on the same worker, and switching accounts throws away the whole prompt cache.
- Stop the local backoff ladder from multiplying a cooldown the upstream declared itself. Transient failures are capped at 60s, and a quota reset reported in the response body is used verbatim (bounded to 24h) instead of being clamped to the 10-minute rate-limit window or scaled up the exponential ladder.
- Keep a session anchor through a short cooldown: a failover whose anchor account is still ready and cooling for five minutes or less no longer clears the binding, so the next turn returns to the cache-warm account instead of walking the pool.
- Raise the default session-affinity TTL from 1h to 4h and expose `CLI2API_AFFINITY_TTL` / `CLI2API_AFFINITY_CAPACITY` (48h and one million bindings are the hard caps).
- Answer a request with no usable account, and an upstream body that cannot be decoded, with `503 no_accounts` / `502 upstream_decode_error` instead of an unclassified `502 upstream_error`.
- Report cache-hit and miss reason distributions (`SessionAffinityStats.miss_reasons` / `escape_reasons`), a per-total `cache_hit_rate`, and per-component session-seed hashes in the affinity debug log, so a chat that silently re-anchors every turn can be traced to a volatile system prefix or a changed first message.

### 中文

- 上游瞬时失败先在同一账号重试一次再切号：首个事件之前就断流或返回空 body 的请求通常在同一 worker 上就能恢复，而换号会把整个 prompt 缓存丢掉。
- 本地指数退避不再放大上游自己给出的冷却时间。瞬时失败封顶 60 秒；响应体里上报的 quota 重置时间按原值使用（上限 24 小时），既不被 10 分钟的限流窗口截断，也不被指数阶梯成倍放大。
- 短冷却期间保留会话锚点：failover 时若原账号仍然 Ready 且冷却不超过 5 分钟，不再清空绑定，下一轮直接回到缓存已预热的账号，而不是重新在池子里轮转。
- 会话亲和默认 TTL 从 1 小时提升到 4 小时，并新增 `CLI2API_AFFINITY_TTL` / `CLI2API_AFFINITY_CAPACITY`（硬上限 48 小时、一百万条绑定）。
- 没有可用账号的请求、以及上游返回无法解码的响应体，分别返回 `503 no_accounts` 和 `502 upstream_decode_error`，不再落到未分类的 `502 upstream_error`。
- 上报缓存命中的 miss/escape 原因分布（`SessionAffinityStats.miss_reasons` / `escape_reasons`）、按总量计算的 `cache_hit_rate`，并在亲和调试日志中输出会话指纹的分量哈希，这样"每轮都在悄悄换号"的对话可以直接定位到是 system 前缀抖动还是首条消息变了。
