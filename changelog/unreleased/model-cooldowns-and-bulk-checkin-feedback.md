### English

- Add a model-cooldown button to every account card. It opens an inline list of models currently in cooldown with a live remaining-time countdown, and shows an empty state when nothing is cooling down.

- Show per-account check-in feedback after a one-click "check in all" run, not only after a single-account check-in. Each card reports success, already checked in, or the raw upstream error with a copy button.

- Remove `baseUrl` and `ignoreDeprecations` from the frontend TypeScript config so the console type-checks under both the workspace TypeScript 6.0 and the globally installed 5.9.

### 中文

- 每个账号卡片新增「模型冷却」按钮，点开可在卡片内查看当前处于冷却中的模型及实时剩余时间；没有模型冷却时显示空状态。

- 一键签到后每个账号卡片也会显示签到反馈（成功 / 已签到 / 失败并展示上游原始错误及复制按钮），此前只有单账号签到才有反馈。

- 移除前端 TypeScript 配置中的 `baseUrl` 与 `ignoreDeprecations`，让工作区 TypeScript 6.0 和全局 5.9 都能通过类型检查。

