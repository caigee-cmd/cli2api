### English

- Auto-refresh stale exhausted quota: the maintenance loop now re-fetches quota every 5 minutes, but only for accounts whose cached snapshot still reports `Exceeded`. A snapshot can go stale when upstream credits are replenished out-of-band (billing reset, add-on purchase) or when a flaky quota endpoint leaves an old `Exceeded=true` in place, so the account stayed out of routing until an operator manually refreshed it. Healthy accounts are never touched by this sweep.
- Quota error instead of "no accounts available": when every account that can serve a model is out of quota, the request now returns `429 insufficient_quota` rather than a generic "no accounts available", so the client gets the real cause.
- Back off quota probing for genuinely exhausted accounts: the maintenance sweep still starts at 5 minutes, but each account that keeps reporting `Exceeded` now steps through 5m → 5m → 5m → 15m → 30m → 1h → 3h → 6h → 12h → 24h instead of being re-fetched every five minutes forever. Provider balances reset on a daily cadence, so a permanently empty account no longer costs ~288 pointless quota calls a day, settling at roughly one probe per day. The ladder resets to the fast end as soon as a snapshot reports a non-zero balance, and a failed probe keeps its level instead of escalating a transient outage into an hours-long wait.

### 中文

- 自动刷新 stale 的已耗尽配额：维护循环现在每 5 分钟重新拉取一次配额，但仅针对缓存快照仍为 `Exceeded` 的账号。快照可能因上游额度被外部恢复（计费重置、加购附加包）或配额接口抖动留下旧的 `Exceeded=true` 而变 stale，导致账号一直被路由排除，直到运维手动刷新才恢复。健康账号不会被此扫描触碰。
- 配额耗尽返回配额错误而非「无可用账户」：当能服务某模型的所有账号都配额耗尽时，请求现在返回 `429 insufficient_quota`，而不是笼统的「无可用账户」，让客户端拿到真实原因。
- 为真正耗尽的账号增加配额探测退避：维护扫描仍从 5 分钟起步，但连续报告 `Exceeded` 的账号会依次退避到 5m → 5m → 5m → 15m → 30m → 1h → 3h → 6h → 12h → 24h，而不是永远每 5 分钟重拉一次。供应商额度一般按天重置，长期为空的账号不会再每天产生约 288 次无效配额调用，最终稳定在每天约 1 次探测。一旦快照报告非零余额，退避立即回到最快档；探测失败则保持当前档位，不会把瞬时故障升级成数小时的等待。
