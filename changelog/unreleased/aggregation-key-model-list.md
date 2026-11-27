### English

- Fix the key editor's cross-provider aggregation switch: while it is on, every provider checkbox under it is disabled and visibly greyed out instead of staying clickable, so a saved grant set cannot drift while the key is in provider-neutral mode.
- Fix an aggregation key returning an empty `/v1/models`: the key now lists the enabled logical models from the model pool plus the account aliases it can reach, still folded into provider-neutral names. The gateway also accepts those logical models on the chat path, not just account aliases, so a client that picks a listed model no longer gets `model_not_aggregated`.

### 中文

- 修复密钥编辑页的跨供应商聚合开关：开启后下方所有供应商复选框置灰且不可选择，避免密钥处于供应商中立模式时仍能改动已保存的供应商授权。
- 修复聚合密钥的 `/v1/models` 为空：现在会返回模型池里已启用的跨供应商聚合模型，以及这把密钥可访问账号上配置的公共别名，并继续折叠成不显示供应商的名称；网关在对话链路也会接受这些聚合模型（不再只认账号别名），客户端从列表选中模型后不会再收到 `model_not_aggregated`。
