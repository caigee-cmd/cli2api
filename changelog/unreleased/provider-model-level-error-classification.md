### English

- Fixed model-level errors being mistaken for account-level problems across providers. Qoder's `10605` / `RESOURCE_LIMIT` model queue is now recognized in the worker path too (`upstream_queued`: no cooldown, no failover, Retry-After relayed), matching the direct path. A bare `1005` is no longer hard-coded as account quota (Trae uses it for model-plan entitlement). WorkBuddy/Devin `404`, Meta/Zhipu/Codex "not available for your plan" / "does not have access" errors are now classified as `model_not_available`, so the affected model is evicted from only that account's routing catalog, the account stays healthy, other accounts still serve, and `/v1/models` stops advertising a model no reachable account can serve. Devin `resource_exhausted` stays model-scoped rate_limit; CodeArts queue errors no longer cool or fail over the account.

### 中文

- 修复了多个接入中"模型级错误被误判为账号级问题"的情况。Qoder 的 `10605` / `RESOURCE_LIMIT` 模型排队现在在 worker 路径也能正确识别（`upstream_queued`：不冷却账号、不切换账号、透传 Retry-After），与直连路径一致。裸 `1005` 不再被硬编码为账号配额（Trae 用它表示套餐模型未解锁）。WorkBuddy/Devin 的 `404`、Meta/Zhipu/Codex 的 "not available for your plan" / "does not have access" 等错误现在归类为 `model_not_available`：只从该账号的路由目录剔除该模型，账号保持健康，其它账号仍可服务，`/v1/models` 不再展示没有可达账号能提供的模型。Devin 的 `resource_exhausted` 保持模型级限流；CodeArts 的排队错误不再冷却或切换账号。
