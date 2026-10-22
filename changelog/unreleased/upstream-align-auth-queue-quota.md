### English

- WorkBuddy: an HTTP 401 on chat now refreshes the token once and retries the same account; only a failed refresh surfaces as an auth error (failover / re-login). TLS certificate verification failures are reported with a trust-store hint and are no longer retried or failed over to other accounts.
- WorkBuddy: `X-Private-Data` is configurable via `WORKBUDDY_PRIVATE_DATA=auto|true|false` (default `auto`: CLI identity sends `false` for personal and `true` for enterprise accounts; explicit values override every identity).
- Qoder: a 403/401 with code 10605 (upstream queue) no longer cools down, kills, or fails over the account (session affinity is kept); the client gets 429 with `Retry-After` from `retryAfterSeconds`. HTTP 430 is surfaced as "client version too low".
- Qoder: optional per-account traffic guard, off by default: `QODER_TRAFFIC_GUARD=on` (concurrency `QODER_TRAFFIC_GUARD_CONCURRENCY`, default 1; start interval `QODER_TRAFFIC_GUARD_INTERVAL_MS`, default 400).
- Devin: daily and weekly usage-quota exhaustion are told apart; without a reset hint the cooldown follows that window's reset time from GetUserStatus, and accounts cooled by a chat-path quota error are re-probed so the cooldown lifts early once usage has recovered.

### 中文

- WorkBuddy：对话遇到 HTTP 401 时先刷新一次 token 并在同一账号重试；只有刷新失败才按鉴权错误处理（换号 / 需重新登录）。TLS 证书校验失败会给出信任证书提示，不再重试、也不再换号重试。
- WorkBuddy：`X-Private-Data` 可通过 `WORKBUDDY_PRIVATE_DATA=auto|true|false` 配置（默认 `auto`：CLI 身份个人号发 `false`、企业号发 `true`；显式取值对所有身份生效）。
- Qoder：带 10605 的 403/401（上游排队）不再冷却、判死或换号（保留会话亲和），客户端收到 429 并按 `retryAfterSeconds` 返回 `Retry-After`；HTTP 430 显示为"客户端版本过低"。
- Qoder：新增可选的单账号流量守卫，默认关闭：`QODER_TRAFFIC_GUARD=on`（并发 `QODER_TRAFFIC_GUARD_CONCURRENCY`，默认 1；请求间隔 `QODER_TRAFFIC_GUARD_INTERVAL_MS`，默认 400）。
- Devin：区分日额度与周额度耗尽；无重置提示时按 GetUserStatus 中对应窗口的重置时间冷却，并会对因对话额度错误冷却的账号重新探测，额度确认恢复后提前解除冷却。
