### English

- Behavior: an account that runs out of quota is cooled down until the next local midnight and the request moves to another account. The response stays `insufficient_quota` only when every account that can serve the model is exhausted, and it then says `all accounts that can serve <model> are on quota cooldown`.

### 中文

- 行为变化：单个账号额度耗尽时会冷却到本地时间次日零点，请求改由其他账号承接。只有当所有能服务该模型的账号都耗尽时，响应才保持 `insufficient_quota`，文案为「all accounts that can serve <model> are on quota cooldown」。
