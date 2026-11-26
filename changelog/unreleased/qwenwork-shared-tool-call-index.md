### English

- QwenWork turns with several tool calls that share one upstream index (seen on Qwen4-Max) now return each call separately with its own arguments, so `/v1/responses` streams finish with `response.completed` instead of an invalid-JSON error.

### 中文

- 千问办公上游在同一轮内用同一个 index 返回多个工具调用（Qwen4-Max 可见）时，现在会拆成各自独立、参数完整的调用，`/v1/responses` 流式请求能正常发出 `response.completed`，不再报参数 JSON 无效。
