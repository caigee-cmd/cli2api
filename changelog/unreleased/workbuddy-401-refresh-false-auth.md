### English

- WorkBuddy chat no longer treats every post-401 token refresh failure as `login_required`: only real session-dead / auth refresh failures keep that path; transport and 5xx refresh errors stay `unavailable` with a short pool cooldown and a diagnostic Observe entry.

### 中文

- WorkBuddy 聊天在 401 后刷新 token 失败时不再一律标成 `login_required`：仅真实 session-dead / auth 刷新失败保留该路径；运输错误与 5xx 刷新失败保持 `unavailable`，走池短冷却，并用现有 Observe 记下可诊断原因。
