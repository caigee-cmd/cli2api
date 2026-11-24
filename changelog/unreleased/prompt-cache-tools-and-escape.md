### English

- Normalize the tools array before hashing the session seed, so a client that rebuilds its tool list in a different order no longer scatters one conversation across accounts. `CLI2API_TOOLS_NORMALIZE=1` additionally normalizes the outbound payload order and protects the upstream cached prefix; the default keeps the payload byte-identical.
- Escape a sticky provider binding when every account of that provider is cooling for a real reason (quota, or a retry hint of two minutes or more) and another provider family can serve the model, instead of answering `429` while a healthy supplier sits idle. Shorter cooldowns still return a Retry-After so the chat keeps its warm cache.

### 中文

- 计算会话指纹前先归一化 tools 数组顺序，客户端每轮重建工具列表不再把同一段对话打散到多个账号。设置 `CLI2API_TOOLS_NORMALIZE=1` 会同时归一化发往上游的 payload 顺序，进一步保护上游的缓存前缀；默认保持 payload 字节不变。
- 当某供应商所有账号都在因为真实原因冷却（额度耗尽，或上游给出两分钟以上的重试提示），而另一供应商家族还能服务该模型时，允许脱离 sticky 绑定改走可用供应商，不再一边返回 `429` 一边让健康账号闲置。更短的冷却仍然返回 Retry-After，让对话保住已预热的缓存。
