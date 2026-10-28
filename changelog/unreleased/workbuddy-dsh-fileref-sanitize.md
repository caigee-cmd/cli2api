### English

- WorkBuddy outbound sanitize now rewrites the DSH file-reference sentence (`paths the user` → `paths that the user`) on system / developer / assistant messages, matching hub v1.9.0 / #684 so the full FILE_REFERENCE_PROMPT block no longer trips 11-128. User and tool messages stay untouched.

### 中文

- WorkBuddy 出站清洗对齐 hub v1.9.0 / #684：对 system / developer / assistant 中的 DSH 文件引用句补入 `that`（`paths the user` → `paths that the user`），避免整段 FILE_REFERENCE_PROMPT 触发 11-128；user / tool 消息保持原样。
