### English

- Trae and Qoder chats now reuse one upstream session across the turns of a conversation, so a repeated prefix can hit the prompt cache instead of being billed in full on every turn. A request with no session still gets a fresh id.

### 中文

- Trae 和 Qoder 的同一段对话现在会复用同一个上游会话，重复的前缀可以命中提示缓存，而不再是每一轮都按全价计费。没有会话的请求仍会使用新的 id。
