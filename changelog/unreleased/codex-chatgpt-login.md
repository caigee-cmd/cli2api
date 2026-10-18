### English

- Codex accounts can Sign in with ChatGPT separately from Codex CLI OAuth, so website logins refresh and chat on the matching endpoints.
- Codex SSE now joins a `data:` field split across lines, so a split `response.completed` still reports usage.
- Codex tool schemas drop `minimum` / `maximum` bounds outside signed 64-bit integers, avoiding upstream HTTP 500s from MCP tools.
- Devin accounts leave quota cooldown early once daily and weekly usage windows show remaining quota again.
- Clear WorkBuddy account and model cooldowns after a successful or already-completed check-in.

### 中文

- Codex 账号可与 CLI OAuth 分开使用 Sign in with ChatGPT，网页登录的凭证会走对应的刷新和聊天端点。
- Codex SSE 会把拆成多行的 `data:` 拼回去，拆开的 `response.completed` 仍能带上用量。
- Codex 工具 schema 会去掉超出有符号 64 位整数的 `minimum` / `maximum`，避免 MCP 工具触发上游 HTTP 500。
- Devin 账号在日/周额度窗口恢复有余量后，会提前结束额度冷却。
- WorkBuddy 签到成功或确认今日已签到后，清除账号级和模型级冷却。
