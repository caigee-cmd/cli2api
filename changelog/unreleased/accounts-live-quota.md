### English

- The accounts page and `GET /api/accounts?refresh=1` now read quota and status live from the upstream on every page load; the stored projection is only used for polling between page loads.
- When an upstream quota read fails, the account card shows "Quota not readable now" together with the upstream error instead of a stale number, so a logged-in account is never reported with an out-of-date balance.
- The "quota snapshot is N minutes old" labels are removed; nothing on the account surface presents a cached balance as the current one.

### 中文

- 账号页与 `GET /api/accounts?refresh=1` 在每次进入页面时都实时读取上游额度与状态；本地投影只在页面停留期间的轮询中使用。
- 上游额度读取失败时，账号卡片显示「暂时读不到额度」并附上上游错误，不再拿旧数字充当当前余额，登录态账号不会被误报成过期余额。
- 删除「额度快照已过期 N 分钟」一类文案，账号相关界面不再把缓存余额当作当前值展示。
