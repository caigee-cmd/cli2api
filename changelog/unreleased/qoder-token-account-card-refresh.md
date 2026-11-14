### English

- Qoder token (PAT) accounts now refresh their status and credits like browser-login accounts. The in-process account had no online probe, so "Refresh credits", opening the Accounts page, and the background sweep never re-read the upstream quota and the card showed a number that never moved. It now probes the live session and pulls `/api/v2/quota/usage` synchronously on a manual refresh (surfacing the error instead of a stale number), and the cached snapshot keeps its real fetch time instead of restamping "now". Quota buckets now follow the same rules as the browser path (an unusable organization package no longer inflates the balance). Token accounts also record their credential type (`pat`) and gateway uid, so the UID and export entry appear as they do for a browser login.

### 中文

- Qoder token（PAT）账号现在会像浏览器登录账号一样刷新状态和积分。进程内账号原本没有任何在线探测，导致「刷新积分」、进入账号页以及后台巡检都不会重新拉取上游额度，卡片上的数字一直不变。现在会探测实时会话，并在手动刷新时同步拉取 `/api/v2/quota/usage`（失败时返回错误而不是保留旧数字），缓存快照也会保留真实的抓取时间而不再每次都写成「当前时间」。额度分桶的合并规则也与浏览器登录路径保持一致（不可用的组织资源包不再虚增余额）。token 账号还会记录凭据类型（`pat`）和网关 uid，因此 UID 与导出入口会和浏览器登录一致。
