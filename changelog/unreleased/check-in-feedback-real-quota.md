### English

- Show per-account check-in feedback on the account card: a green check on success, a yellow circle when already checked in, and a red cross with the raw upstream error (truncated, with a copy button for the full text) on failure. The notice auto-dismisses after three seconds.
- Pull real upstream quota on every account page entry and after each manual/one-click/scheduled check-in, so the card always reflects the true credit total and usage.
- Remove fallback quota synthesis: WorkBuddy totals/usage now come only from the upstream billing packs, Command no longer derives a monthly total from a local plan table, Qoder refreshes the OpenAPI quota (including the check-in add-on grant) after check-in, and Codex reports no quota instead of reusing a cached value when the upstream does not expose one.

### 中文

- 账号卡片现在显示每账号的签到反馈：成功显示绿色对勾，已签到显示黄色圆圈，失败显示红色叉号并展示上游返回的原始错误信息（超出范围则截断，并提供复制按钮复制完整内容），提示三秒后自动消失。
- 每次进入账号页以及手动/一键/定时签到后都会主动拉取上游真实额度，确保卡片展示的积分总上限与已用额度始终是最新真实数据。
- 移除额度兜底计算：WorkBuddy 的总量与已用只来自上游计费包，Command 不再用本地 plan 表派生月度上限，Qoder 在签到后刷新 OpenAPI 额度（含签到 add-on 奖励），Codex 在无法从上游获取额度时改为显示无额度而非复用缓存值。
