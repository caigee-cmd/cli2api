### English

- Qoder requests now resolve the requested model name or display name to the account's gateway key from the cached model catalog, so replies come from the model you asked for; an unknown model returns `model_not_found` instead of being sent upstream raw.
- Qoder model key mappings are replaced on each catalog refresh, so keys Qoder has reassigned no longer route to the wrong model.

### 中文

- Qoder 请求现在会根据缓存的模型目录把请求的模型名或显示名解析为账号的网关 key，回复来自所请求的模型；未知模型返回 `model_not_found`，不再原样发往上游。
- 每次刷新模型目录时都会替换 Qoder 模型 key 映射，Qoder 复用过的 key 不会再路由到错误的模型。
