### English

- Console: the account card shows the result of a check-in. Success is a green check, already checked in is a yellow circle, and a failure is a red cross with the upstream error. A long error is shortened on the card and can be copied in full. The notice goes away after three seconds. Opening the accounts page, and every manual, one-click, or scheduled check-in, refreshes the account's quota from upstream.
- Fix: Qoder check-in sends the claim again when a credit grant is still inside its window, instead of trusting a claimed row from a previous day. A replay stays already checked in, and a new claim records today's credits. A result of granted or success counts as collected. The legacy worker check-in now reports the same desktop version as the in-process check-in.
- Fix: WorkBuddy keeps the declared tools when a request sets tool choice to none. Dropping them made a long session invent the next call.
- Fix: Devin's last MCP retry keeps the tools the client declared and drops their descriptions. It no longer substitutes Codex tools the client cannot run, and it returns the original denial when no declared tools remain.
- Behavior: Codex requests present the official stable CLI identity, `codex_cli_rs/0.167.2`. A request that selects the priority or ultrafast tier also says so on the routing hint.
- Behavior: quota snapshots no longer mark an account exhausted from a partial read. WorkBuddy fills a missing used count from the pack size and raises the total when the account dosage is larger, but an empty pack list stays unknown. Command Code shows the upstream 5-hour and weekly windows, and a spent 5-hour window does not block the account while the weekly window has credits. A failed Codex usage probe keeps the windows last reported by a chat response. A ChatGPT sign-in has no usage probe and does not replay an old window.

### 中文

- 控制台：账号卡片会显示签到结果。成功是绿色对勾，今日已签到是黄色圆圈，失败是红色叉号并带上游错误。过长的错误会在卡片上截断，完整内容可以复制。提示三秒后消失。进入账号页，以及每次手动、一键或定时签到之后，都会重新拉取该账号的上游额度。
- 修复：Qoder 签到在积分活动仍处于窗口内时会重新领取，不再把前一天留下的已领取记录当成今日已签到。幂等回放仍记为已签到，新的领取记为今日到账。结果为 granted 或 success 时也记为已领取。旧的 worker 签到现在与进程内签到使用同一个桌面版本。
- 修复：WorkBuddy 在请求指定不使用工具时会保留已声明的工具。以前把工具删掉后，长会话会自己编造下一次调用。
- 修复：Devin 最后一次 MCP 重试会保留客户端声明的工具，并去掉工具描述。它不再换成客户端无法执行的 Codex 工具；如果没有声明任何工具，就返回原来的拒绝。
- 行为变化：Codex 请求使用官方稳定版 CLI 身份 `codex_cli_rs/0.167.2`。请求选择了 priority 或 ultrafast 档位时，路由提示里也会带上这个档位。
- 行为变化：额度快照不再因为一次不完整的读取把账号判成额度耗尽。WorkBuddy 会用包大小补上缺失的已用数，并在账号总量更大时抬高总量，但包列表为空时保持未知。Command Code 显示上游的 5 小时和每周窗口，5 小时窗口用完时，只要每周窗口还有额度，账号就不会被挡住。Codex 用量探测失败时保留上一次聊天响应记下的窗口。ChatGPT 登录没有用量探测，不再把旧窗口当成当前额度。
