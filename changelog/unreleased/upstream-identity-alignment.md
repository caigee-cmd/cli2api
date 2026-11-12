### English

- Qoder check-in no longer treats a claimed credit grant as already collected for today. When the grant is still inside its window, the claim is sent again: a replay stays already collected, and a fresh claim records today's credits. A claim that comes back as granted or success counts as collected.
- WorkBuddy keeps the tool list when a request sets tool choice to none, instead of deleting the tools and leaving a long session to invent the next call.
- Codex requests now present the official stable CLI identity, `codex_cli_rs/0.167.2`. A request that selects the priority or ultrafast tier also says so on the routing hint.

### 中文

- Qoder 签到不再把列表里已领取的积分活动直接当成今日已签到。活动仍在窗口内时会重新领取一次：幂等回放仍记为已签到，新的领取则记为今日到账。领取结果返回 granted 或 success 时也记为已领取。
- WorkBuddy 在请求指定不使用工具时会保留工具列表，不再把工具删掉，避免长会话自己编造下一次调用。
- Codex 请求改为使用官方稳定版 CLI 身份 `codex_cli_rs/0.167.2`。请求选择了 priority 或 ultrafast 档位时，路由提示里也会带上这个档位。
