### English

- Fix: WorkBuddy request-body sanitization strips Claude Code billing attribution headers (`x-anthropic-billing-header` and `x-anthropic-billing-hdr`). Those reserved keys used to go upstream as-is and were rejected with 400 / 11128. A system message that is only that header is dropped, and a leading system prompt is restored when needed so the empty text does not trigger 400 / 11151.

### 中文

- 修复：WorkBuddy 请求体清洗会去掉 Claude Code 的计费归因头（`x-anthropic-billing-header` 与 `x-anthropic-billing-hdr`）。这两个保留键原先会原样出站，上游直接回 400 / 11128。若整条 system 消息只剩该头，会丢弃该消息，并在需要时补回一条 leading system，避免空文本触发 400 / 11151。
