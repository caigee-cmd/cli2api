### English

- Add the upstream prompt-cache marker (ephemeral `cache_control`) to Qoder chat requests, so repeated context prefixes hit the gateway's prompt cache. Qoder CN accounts previously paid uncached rates for the full context on every call.
- Bound the buffered (non-stream) Qoder chat path with the same upstream first-byte deadline as streaming: a black-holed gateway connection now fails over within the configured TTFB window instead of hanging until the kernel TCP timeout.

### 中文

- Qoder 聊天请求现在会携带上游提示缓存标记（ephemeral `cache_control`），重复的上下文前缀将命中网关提示缓存。此前 Qoder CN 账号每次调用都按未缓存全价计费整个上下文。
- 非流式 Qoder 聊天路径现在与流式路径共用上游首字节超时：网关连接被静默黑洞时，会在配置的 TTFB 窗口内切换账号，而不是挂到内核 TCP 超时。
