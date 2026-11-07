### English

- Add the upstream prompt-cache marker (ephemeral `cache_control`) to Qoder chat requests, so repeated context prefixes hit the gateway's prompt cache. Qoder CN accounts previously paid uncached rates for the full context on every call.

### 中文

- Qoder 聊天请求现在会携带上游提示缓存标记（ephemeral `cache_control`），重复的上下文前缀将命中网关提示缓存。此前 Qoder CN 账号每次调用都按未缓存全价计费整个上下文。
