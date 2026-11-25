### English

- Fix the key editor's cross-provider aggregation switch: while it is on, every provider checkbox under it is disabled and visibly greyed out instead of staying clickable, so a saved grant set cannot drift while the key is in provider-neutral mode.
- Fix an aggregation key returning an empty `/v1/models`: the key now lists the enabled logical models from the model pool plus the account aliases it can reach, still folded into provider-neutral names. The gateway also accepts those logical models on the chat path, not just account aliases, so a client that picks a listed model no longer gets `model_not_aggregated`.
- Keep the synthetic `aggregation` provider out of every shared catalog, console and server: provider settings, account model directories and the model-pool member picker show only real upstream models, and the ordinary `/v1/models` response no longer merges logical entries or hides their real members. An ordinary key always routes the real provider model, so a logical ID that reuses a real catalog ID can never shadow it. Logical models appear only on the model-pool page and in the key-scoped `/v1/models` view of a key with aggregation enabled.

### 中文

- 修复密钥编辑页的跨供应商聚合开关：开启后下方所有供应商复选框置灰且不可选择，避免密钥处于供应商中立模式时仍能改动已保存的供应商授权。
- 修复聚合密钥的 `/v1/models` 为空：现在会返回模型池里已启用的跨供应商聚合模型，以及这把密钥可访问账号上配置的公共别名，并继续折叠成不显示供应商的名称；网关在对话链路也会接受这些聚合模型（不再只认账号别名），客户端从列表选中模型后不会再收到 `model_not_aggregated`。
- 控制台和服务端的共享模型目录都不再混入合成的 `aggregation` 供应商：供应商设置、账号模型目录和模型池成员选择器只显示真实上游模型，普通密钥的 `/v1/models` 不再合并逻辑条目、也不再隐藏其成员模型；普通密钥始终路由真实供应商模型，逻辑 ID 即使与真实目录 ID 同名也不会把后者顶掉。逻辑模型只出现在模型池页面，以及开启聚合的密钥自己的 `/v1/models` 中。
