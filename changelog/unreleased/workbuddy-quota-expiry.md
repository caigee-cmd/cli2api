### English

- Expose WorkBuddy credit-pack expiry in `GET /api/accounts` quota: a new `packages` array carries each pack's remain/used/size plus its `CycleEndTime` (as `end_time` and a Unix `ends_at`), and the top-level `expires_at` / `expiring_remain` report the soonest expiry and how much remaining credit lapses then. The console quota tooltip now shows "N credits expire on D". All fields are `omitempty`; providers that do not report expiry (Trae, Qoder) simply omit them.

### 中文

- 在 `GET /api/accounts` 的 quota 中透出 WorkBuddy 积分包到期时间：新增 `packages` 数组按包返回 remain/used/size 以及 `CycleEndTime`（`end_time` 原始串与 Unix 秒 `ends_at`），顶层 `expires_at` / `expiring_remain` 给出最近一次到期时间及该时点将过期的剩余量；控制台配额 tooltip 现在会显示「N 积分将于某日到期」。所有字段均为 `omitempty`，不上报到期信息的 provider（Trae、Qoder）保持缺省。
