### English

- Add a configurable credit floor (`CLI2API_CREDIT_FLOOR`, default `0` = disabled) so accounts below the threshold are skipped for paid models while remaining eligible for free models.
- Allow a per-account `credit_floor` override in the account card (save an explicit `0` to disable the floor for that account; leave the override off to inherit the global floor) and through the management API.
- Only enforce the credit floor against quota snapshots reported in `credits`.
- Add an independent percent floor (`CLI2API_PERCENT_FLOOR`, default `0` = disabled, per-account `percent_floor`) for accounts whose quota is reported as a remaining percentage (Devin, Kimi, Zhipu). The two floors never compare across units; entitlement-pack and rate-limit-window accounts keep their existing scheduling behaviour.
- Reject invalid or negative floor values at startup instead of silently disabling the gate.

### 中文

- 新增可配置积分下限（`CLI2API_CREDIT_FLOOR`，默认 `0` 表示关闭）：积分低于阈值的账号不再调度计费模型，但仍可调度免费模型。
- 账号卡片与管理 API 支持按账号设置 `credit_floor`（填 0 表示显式关闭该账号保底；关闭覆盖则继承全局阈值）。
- 积分阈值只对以 `credits` 为单位上报的余额生效。
- 新增独立的百分比保底（`CLI2API_PERCENT_FLOOR`，默认 `0` 关闭；按账号 `percent_floor`），适用于以剩余百分比上报余额的账号（Devin、Kimi、智谱）；两种单位分别判断、互不做数值换算，权益包与速率窗口账号维持原有调度行为。
- 启动时会拒绝非法或负值配置，避免阈值校验被静默关闭。
