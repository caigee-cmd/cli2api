### English

- With the cross-provider model pool disabled, the model list endpoints (`/api/models` and `/v1/models`) now serve provider-prefixed model IDs, matching what the gateway accepts; previously the lists still showed bare IDs that every request using them would get rejected with `provider_prefix_required`, breaking the console access page and OpenAI-compatible clients.

### 中文

- 关闭跨 Provider 模型池后，模型列表接口（`/api/models` 与 `/v1/models`）现在返回带 Provider 前缀的模型 ID，与网关校验保持一致；此前列表仍是裸 ID，按列表选择的请求都会被 `provider_prefix_required` 拒绝，导致控制台接入与调试页面及 OpenAI 兼容客户端不可用。
