### English

- Added a "Cross-provider model pool" page between Models and Access. It appears only while the existing cross-provider model pool setting is enabled, and toggling that setting updates the navigation immediately.
- You can now define logical models that aggregate multiple real provider models behind one client-facing ID, for both same-named models across providers and differently named models. Each logical model has its own routing strategy (priority order, round robin, fill first, or FEFO), failover settings, and an ordered, weighted member list.
- `/v1/models` now exposes the logical IDs instead of the aggregated per-provider entries; member capabilities are merged conservatively. Logical models are served through the existing route loop and failover/cooldown system, with cooldowns recorded against the real member model. The per-model strategy (priority order by default, round robin, fill first, FEFO) and member weights are honored during routing; priority order keeps using the first healthy member and falls through on failure.

### 中文

- 在“模型”和“接入与调试”之间新增“跨 Provider 模型池”页面；仅当现有的跨 Provider 模型池开关开启时出现，切换开关后导航会立即更新。
- 现在可以定义逻辑模型，把多个真实供应商模型聚合到同一个客户端 ID 下，既支持跨供应商的同名模型，也支持不同名称的模型。每个逻辑模型都可以单独配置路由策略（优先级顺序、轮询、优先填满或 FEFO）、故障转移规则，以及有序带权重的成员列表。
- `/v1/models` 现在返回聚合后的逻辑 ID，而不再展示被聚合的逐供应商条目；成员能力按保守方式合并。逻辑模型复用现有选路与故障转移/冷却体系，冷却记录在真实成员模型上。每个逻辑模型的策略和成员权重现在真正参与选路；优先级顺序会持续使用第一个健康成员并在失败时顺延，未配置时默认按优先级顺序。
