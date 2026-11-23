### English

- An account whose first health probe fails no longer stays in `error` until an operator clicks Refresh. The maintenance watchdog now re-runs the full account refresh for a worker that answers `/health` but is still absent from routing, throttled to once every two minutes per account. This covers both the boot-time probe racing a worker that has not bound its port yet and a worker that came back through crash recovery — either case previously left the account, and every model only it could serve, unrouteable.
- A request no enabled account can serve is now answered with `503 no_accounts` instead of the generic `502 upstream_error` the gateway synthesizes for unclassified failures. That 502 read as an origin outage (a proxy in front of the gateway replaced it with its own Bad gateway page) while the real cause was local routing, and a 429 would have made SDKs auto-retry on a fixed schedule a condition only the account watchdog can heal. The `Retry-After` hint matches the watchdog's two-minute per-account re-probe cadence. A key that cannot use the requested provider gets `403 provider_not_allowed`, and an instance with no accounts at all gets `503 no_accounts_configured`.

### 中文

- 首次健康探测失败的账号不再卡在 `error` 状态等人工点刷新：维护看门狗现在会对「`/health` 有响应但没进路由」的 worker 重跑一次账号刷新，每个账号两分钟最多一次。既覆盖开机时探测早于 worker 绑定端口的竞态，也覆盖崩溃恢复后重新拉起的 worker——这两种情况以前都会让该账号（以及只有它能提供的模型）一直不可路由。
- 没有任何可用账号的请求现在返回 `503 no_accounts`，不再落到网关对未分类错误兜底的 `502 upstream_error`。那个 502 看起来像源站故障（网关前面的代理还会把它替换成自己的 Bad gateway 页），实际原因是本地路由；而 429 会让 SDK 按固定节奏自动重试一个只有账号看门狗才能恢复的问题。`Retry-After` 与看门狗每账号两分钟的重探节奏一致。密钥无权使用请求的 provider 时返回 `403 provider_not_allowed`，实例里一个账号都没有时返回 `503 no_accounts_configured`。
