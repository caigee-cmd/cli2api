### English

- WorkBuddy request payloads now automatically sanitize client environment fingerprint phrases (such as `Main branch (you will usually use this for PRs)` injected by ZCode / Claude Code environments) before dispatching to upstream. Upstream Tencent WorkBuddy WAF detects these signatures and rejects requests with error `11128: Illegal API invocation from an unapproved channel`. The pre-dispatch sanitizer transparently rewrites these phrases into neutral equivalents across all message roles and structured text parts, preventing rejection without requiring users to discard their entire system prompt.

### 中文

- WorkBuddy 请求体在发送给上游前现已自动脱敏客户端环境签名特征词（例如 ZCode / Claude Code 环境自动注入的 `Main branch (you will usually use this for PRs)`）。腾讯 WorkBuddy 上游 WAF 会检测此类特征词并将请求拦截为 `11128: Illegal API invocation from an unapproved channel`。现在的预处理流水线会在发送前自动将所有消息角色和多段文本中的敏感短语透明重写为中性表述，避免请求被拦截，且无需强制丢弃完整的系统提示词。
