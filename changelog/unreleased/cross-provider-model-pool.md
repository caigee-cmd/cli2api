### English

- Added a "Cross-provider model pool" page between Models and Access. It appears only while the existing cross-provider model pool setting is enabled, and toggling that setting updates the navigation immediately.
- You can now define logical models that aggregate multiple real provider models behind one client-facing ID, for both same-named models across providers and differently named models. Each logical model has its own routing strategy (priority order, round robin, fill first, or FEFO), failover settings, and an ordered, weighted member list.
- `/v1/models` now exposes the logical IDs instead of the aggregated per-provider entries; member capabilities are merged conservatively. Logical models are served through the existing route loop and failover/cooldown system, with cooldowns recorded against the real member model. The per-model strategy (priority order, round robin default, fill first, FEFO) and member weights are honored during routing; priority order keeps using the first healthy member and falls through on failure.
- Each API key now has its own "Model aggregation mode" switch in the create/edit dialog. When on, the provider cards are greyed out and the key stores only the aggregation grant, so it can call logical models but cannot pin or directly use real provider models; when off, the key keeps its normal provider selection. Key cards show "Model aggregation mode" instead of a provider list for such keys.

### 中文

- 在“模型”和“接入与调试”之间新增“跨 Provider 模型池”页面；仅当现有的跨 Provider 模型池开关开启时出现，切换开关后导航会立即更新。
- 现在可以定义逻辑模型，把多个真实供应商模型聚合到同一个客户端 ID 下，既支持跨供应商的同名模型，也支持不同名称的模型。每个逻辑模型都可以单独配置路由策略（优先级顺序、轮询、优先填满或 FEFO）、故障转移规则，以及有序带权重的成员列表。
- `/v1/models` 现在返回聚合后的逻辑 ID，而不再展示被聚合的逐供应商条目；成员能力按保守方式合并。逻辑模型复用现有选路与故障转移/冷却体系，冷却记录在真实成员模型上。每个逻辑模型的策略和成员权重现在真正参与选路；优先级顺序会持续使用第一个健康成员并在失败时顺延，默认策略为轮询。
- 每个 API 密钥在创建/编辑弹窗中新增“模型聚合模式”开关：开启后供应商卡片变灰不可选，密钥仅保存聚合授权，可调用逻辑模型但不能固定或直接使用真实供应商模型；关闭时维持原有供应商选择。此类密钥在卡片上显示“模型聚合模式”而非供应商列表。
