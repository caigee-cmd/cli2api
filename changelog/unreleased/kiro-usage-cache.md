### English

- Anthropic Messages streams now report `input_tokens` in the final `message_delta`; it was missing before, so clients saw 0 input. Across all providers, Anthropic `input_tokens` now counts only the uncached part of the prompt, with `cache_read_input_tokens` and `cache_creation_input_tokens` carrying the rest, matching Anthropic's own usage format.
- Kiro requests now report estimated prompt-cache reads and writes. Kiro does not return cache figures, so the gateway tracks prompt prefixes per account for five minutes, the way Kiro-Go does, and reports the repeated part as a cache read. The figures are estimates.
- Kiro output tokens are estimated more closely (separate weights for letters, digits, punctuation and CJK text), and token counts are now read from more upstream usage shapes when Kiro does send them.

### 中文

- Anthropic Messages 流式响应现在会在最后的 `message_delta` 中返回 `input_tokens`，之前缺失导致客户端看到的输入为 0。所有渠道的 Anthropic `input_tokens` 现在只计未命中缓存的部分，其余由 `cache_read_input_tokens` 和 `cache_creation_input_tokens` 表示，与 Anthropic 官方用量格式一致。
- Kiro 请求现在会返回估算的缓存读取和缓存写入。Kiro 本身不返回缓存数据，网关参照 Kiro-Go 按账号记录 5 分钟内的提示前缀，重复部分计为缓存读取。该数值为估算值。
- Kiro 输出 Tokens 的估算更准确（字母、数字、标点和中日韩文字分别计权），Kiro 返回 Token 数时也能识别更多上游用量格式。
