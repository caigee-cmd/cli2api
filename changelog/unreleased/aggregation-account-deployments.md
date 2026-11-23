### English

- Add `account_id` to aggregation members so one logical model can target a specific account/deployment instead of fanning out over every account of a provider.
- Persist the field in SQLite and expose it through the console API and model pool editor.
- Keep legacy members without `account_id` on the existing provider/region pool behavior.

### 中文

- 聚合成员新增 `account_id`，让一个逻辑模型可以固定到一个具体账号/deployment，而不是默认在该供应商下扇出到所有账号。
- 该字段持久化到 SQLite，并通过控制台 API 和模型池编辑器暴露。
- 未填写 `account_id` 的旧成员保持原有 provider/region 池行为。
