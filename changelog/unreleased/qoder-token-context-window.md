### English

- Restore the Qoder context-window control for PAT / token sign-in. Accounts added with a token read `/model/list` in-process, where the selectable windows live in a nested `context_config` object (`200K` / `400K` / `1M`, one tier marked default). The parser only knew the worker's flattened `default_context_window` / `available_context_windows` keys, so every token account lost the "default → larger" switch and the console could not change its context size. The adapter now reads `context_config` too, so token and browser sessions show the same window and switch. Chat requests also send the official `parameters.contextWindow` field alongside the existing `context_length`.

### 中文

- 修复 token（PAT）登录的 Qoder 账号无法调整上下文窗口。token 账号在进程内直接读取 `/model/list`，可选窗口放在嵌套的 `context_config` 里（`200K` / `400K` / `1M`，其中一档标记为默认），而解析器只认识 worker 展平后的 `default_context_window` / `available_context_windows`，因此每个 token 账号都丢了「默认档 → 更大档」开关，控制台无法修改上下文。适配层现在同时读取 `context_config`，token 登录与浏览器登录显示一致的窗口和开关。聊天请求也会在原有 `context_length` 之外补发官方客户端使用的 `parameters.contextWindow`。
