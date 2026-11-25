### English

- Fix the key editor's cross-provider aggregation switch: while it is on, every provider checkbox under it is disabled and visibly greyed out instead of staying clickable, so a saved grant set cannot drift while the key is in provider-neutral mode.
- Fix an aggregation key returning an empty `/v1/models`: the key now lists the enabled logical models from the model pool plus the account aliases it can reach, still folded into provider-neutral names. The gateway also accepts those logical models on the chat path, not just account aliases, so a client that picks a listed model no longer gets `model_not_aggregated`.
- Keep the synthetic `aggregation` provider out of the console catalog: provider settings, account model directories and the model-pool member picker now show only real upstream models, so a logical model that reuses a real ID no longer hides that provider's row or creates a phantom provider card. Logical models stay visible on the model-pool page and in the key-scoped `/v1/models` view.

### 中文

- 修复密钥编辑页的跨供应商聚合开关：开启后下方所有供应商复选框置灰且不可选择，避免密钥处于供应商中立模式时仍能改动已保存的供应商授权。
- 修复聚合密钥的 `/v1/models` 为空：现在会返回模型池里已启用的跨供应商聚合模型，以及这把密钥可访问账号上配置的公共别名，并继续折叠成不显示供应商的名称；网关在对话链路也会接受这些聚合模型（不再只认账号别名），客户端从列表选中模型后不会再收到 `model_not_aggregated`。
- 控制台的模型目录不再混入合成的 `aggregation` 供应商：供应商设置、账号模型目录和模型池成员选择器只显示真实上游模型，复用真实 ID 的逻辑模型不会再遮住该供应商的真实条目，也不会生成一个假的供应商设置卡；逻辑模型仍保留在模型池页面和密钥级 `/v1/models` 中。
