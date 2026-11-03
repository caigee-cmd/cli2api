### English

- CodeArts chat now sends WorkBuddy tip `28527f4` / hub #723 session headers after signing: account-stable `x-ot-session-id` and `user-session-id`, plus a fresh `x-ot-trace-id` per request (kept out of SignedHeaders), so Huawei no longer treats every call as a new concurrent session.

### 中文

- 码道（CodeArts）聊天在签名之后补发 WorkBuddy tip `28527f4` / hub #723 会话头：账号级稳定的 `x-ot-session-id` 与 `user-session-id`，以及每请求新的 `x-ot-trace-id`（不进入 SignedHeaders），避免华为云把每次请求都当成新建并发会话。
