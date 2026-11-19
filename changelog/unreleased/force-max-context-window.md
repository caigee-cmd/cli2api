### English

- Chat requests no longer receive a cli2api-chosen context window. Only a context window supplied by the client is forwarded; catalog context limits, including each model's maximum context, are display-only. Trae max mode is likewise forwarded only when the client asks for it.
- Align reasoning conversion with CC Switch conventions: accept `ultra` as the deepest tier and normalize Anthropic thinking budgets into the catalog-driven reasoning ladder.

### 中文

- 聊天请求不再由 cli2api 约定上下文窗口；只透传客户端传入的上下文窗口。目录中的上下文上限（包括每个模型的最大上下文窗口）仅作展示。Trae 的最大档位同样只按客户端请求透传。
- 推理转换对齐 CC Switch 约定：接受 `ultra` 作为最深档，并把 Anthropic thinking budget 归一化到由 catalog 驱动的推理档位。
