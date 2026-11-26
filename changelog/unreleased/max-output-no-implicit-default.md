### English

- Reverted the implicit max-output default: a request that omits `max_tokens` is passed through to the upstream again, and the gateway only publishes the model's limits in `/v1/models` instead of forcing the model's maximum output size.

### 中文

- 回退了隐式最大输出兜底：客户端不传 `max_tokens` 时重新原样透传给上游，网关只在 `/v1/models` 里声明模型的上限，不再替客户端把输出上限拉到模型最大值。
