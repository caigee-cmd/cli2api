### English

- Aggregated models can now pin the context window, max output tokens, reasoning levels, and default reasoning effort in the model pool editor; `/v1/models` advertises exactly those values.
- Aggregations without overrides advertise the conservative intersection of their members (smallest window and output, shared reasoning levels) instead of the first member's values, and members are resolved by provider and region rather than by model name alone.
- Requests that carry no reasoning intent use the aggregation's configured default level, and each member provider still maps or clamps the level to what its own model supports.

### 中文

- 聚合模型可在模型池编辑器中配置上下文窗口、最大输出、思考档位与默认思考强度，`/v1/models` 会按配置值对外声明。
- 未配置时聚合改为声明各成员保守的交集（最小窗口与输出、共有的思考档位），不再沿用第一个成员的数值；成员按供应商与区域精确解析，而不是只按模型名匹配。
- 未携带思考意图的请求会使用聚合配置的默认档位，每个成员的供应商仍按自身模型支持的档位做映射与钳制。
