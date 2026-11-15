### English

- Trae and Qoder chats now reuse one upstream session across the turns of a conversation, so a repeated prefix can hit the prompt cache instead of being billed in full on every turn. A request with no session still gets a fresh id.
- Qoder chat now presents client version 1.1.81 and names the model on the request the upstream uses for billing, so a model is no longer charged as auto.
- Qoder parallel tool calls stay separate when the upstream repeats the same index. Split arguments of one call still join back together.

### 中文

- Trae 和 Qoder 的同一段对话现在会复用同一个上游会话，重复的前缀可以命中提示缓存，而不再是每一轮都按全价计费。没有会话的请求仍会使用新的 id。
- Qoder 对话现在以 1.1.81 的客户端版本出站，并在上游用来记账的字段里带上模型名，模型不再被记成 auto。
- Qoder 的并行工具调用在上游重复同一个序号时仍会分开。同一次调用拆开的参数仍会拼回去。
