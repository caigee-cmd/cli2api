### English

- A client disconnect while the gateway is still waiting for the first upstream event is reported as a cancellation. It no longer counts as a successful response or clears the account's cooldown.
- An account that runs out of quota is cooled down until the next local midnight and the request moves to another account. The response stays `insufficient_quota` only when every account that can serve the model is exhausted.

### 中文

- 网关还在等上游第一个事件时客户端断开，现在记为取消，不再算作成功响应，也不会清掉该账号已有的冷却。
- 单个账号额度耗尽时会冷却到本地时间次日零点，请求改由其他账号承接。只有当所有能服务该模型的账号都耗尽时，响应才保持 `insufficient_quota`。
