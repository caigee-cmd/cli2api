### English

- Back off quota probing for genuinely exhausted accounts: the maintenance sweep still starts at 5 minutes, but each account that keeps reporting `Exceeded` now steps through 5m → 5m → 5m → 15m → 30m → 1h → 3h → 6h → 12h → 24h instead of being re-fetched every five minutes forever. Provider balances reset on a daily cadence, so a permanently empty account no longer costs ~288 pointless quota calls a day, settling at roughly one probe per day. The ladder resets to the fast end as soon as a snapshot reports a non-zero balance, and a failed probe keeps its level instead of escalating a transient outage into an hours-long wait.

### 中文

- 为真正耗尽的账号增加配额探测退避：维护扫描仍从 5 分钟起步，但连续报告 `Exceeded` 的账号会依次退避到 5m → 5m → 5m → 15m → 30m → 1h → 3h → 6h → 12h → 24h，而不是永远每 5 分钟重拉一次。供应商额度一般按天重置，长期为空的账号不会再每天产生约 288 次无效配额调用，最终稳定在每天约 1 次探测。一旦快照报告非零余额，退避立即回到最快档；探测失败则保持当前档位，不会把瞬时故障升级成数小时的等待。
