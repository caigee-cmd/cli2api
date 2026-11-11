### English

- Behavior: WorkBuddy chat now identifies as CodeBuddy CLI 2.168.2. Requests send that version, the official CLI's OpenAI SDK fingerprint, and `cache_retention: long` when the caller did not set one. `X-Session-ID` and `X-Product-Version` are no longer sent; `X-Root-Request-ID`, `X-Agent-Purpose`, and `X-Agent-Runtime` are. The international desktop catalog identity moves to 5.9.4 (bundled CLI 2.165.0); the CN desktop identity is unchanged.

### 中文

- 行为变化：WorkBuddy 对话身份对齐到 CodeBuddy CLI 2.168.2。请求现在发送该版本号、官方 CLI 的 OpenAI SDK 指纹，并在调用方未指定时补上 `cache_retention: long`。不再发送 `X-Session-ID` 与 `X-Product-Version`，改为发送 `X-Root-Request-ID`、`X-Agent-Purpose` 与 `X-Agent-Runtime`。国际版桌面目录身份升到 5.9.4（内置 CLI 2.165.0），CN 桌面身份不变。
