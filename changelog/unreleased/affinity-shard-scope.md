### English

- `CLI2API_MSGSTAT_SHARDS` no longer replaces content-seed session affinity. It only pins a request that has no conversation id and no user message, so distinct chats that share a system prefix stay on their own accounts.
- `CLI2API_AFFINITY_DEBUG` now logs upstream prompt and cache-token hits for `/v1/messages` and `/v1/responses` as well as `/v1/chat/completions`.
- WorkBuddy WAF fingerprint detection now scans structured message content, including raw JSON and nested objects, so a fingerprint outside a plain string is still rewritten.

### 中文

- `CLI2API_MSGSTAT_SHARDS` 不再覆盖内容指纹会话粘性。它只钉住没有会话 ID、也没有用户消息的请求，共用同一段 system 前缀的不同对话仍各自留在自己的账号上。
- `CLI2API_AFFINITY_DEBUG` 现在也会为 `/v1/messages` 和 `/v1/responses` 记录上游 prompt 与缓存 token 命中，不再只覆盖 `/v1/chat/completions`。
- WorkBuddy WAF 指纹检测现在会扫描结构化消息内容，包括原始 JSON 和嵌套对象，纯字符串之外的指纹也会被改写。
