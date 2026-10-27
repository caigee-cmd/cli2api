### English

- Keep Trae caller system prompts by default, forward `response_format`, and report `finish_reason=tool_calls` when Solo emits tools with `stop`, so clients such as MiMo Code do not abort mid-loop.

### 中文

- Trae 默认保留调用方系统提示词，转发 `response_format`，并在 Solo 带着工具调用却返回 `stop` 时改报 `finish_reason=tool_calls`，避免 MiMo Code 等客户端中途停掉。
