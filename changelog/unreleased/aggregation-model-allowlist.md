### English

- Cross-provider model pool saves now validate every member against the provider policy. A disabled model is rejected instead of being stored as a dead route.
- An aggregated logical model is no longer listed in `/v1/models` when provider policy filters out every one of its members.

### 中文

- 保存跨 Provider 模型池时会逐个校验成员是否符合供应商策略。未启用的模型会被拒绝，不会再存成无法路由的死成员。
- 当供应商策略把所有成员都过滤掉时，聚合逻辑模型不再出现在 `/v1/models` 中。
