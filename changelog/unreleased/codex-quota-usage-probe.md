### English

- Fix Codex quota refresh: the usage probe now calls the backend-api `/wham/usage` endpoint instead of the chat-prefixed `/codex/wham/usage` path, which answers with the Cloudflare 403 interstitial. An account whose last snapshot says "exhausted" refreshes its windows again instead of staying quota-blocked on stale numbers, and a probe failure with no cached windows is reported instead of silently ignored.

### 中文

- 修复 Codex 额度刷新：用量探测改走 backend-api 的 `/wham/usage`，不再请求聊天前缀下的 `/codex/wham/usage`（那里返回的是 Cloudflare 403 拦截页）。上次快照显示「额度耗尽」的账号现在会重新拉取窗口数据，不再一直卡在过期数字上；探测失败且没有缓存窗口时会如实报错，而不是静默忽略。
