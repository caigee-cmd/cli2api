### English

- Tool calls: stop dropping or failing on argument text that is only formatting-damaged. Trailing commas, bare quotes inside a value, missing closing brackets, and prose appended after the object are now repaired before a call is validated, on `/v1/responses`, `/v1/messages`, and the OpenAI compatibility paths.
- Streaming assembly no longer mangles arguments when an upstream resends the whole argument buffer on each chunk, omits `index`, reports `index` 0 for every call, or sends a JSON object where the schema expects a string. Those cases previously produced `{"a":1}{"a":1}` or silently lost fragments, which clients report as "arguments are invalid JSON".
- Arguments genuinely cut mid-string (output-token limit) are still rejected instead of being padded into a valid-looking command, and the error now says the model hit its token limit.

### 中文

- 工具调用：参数文本只是格式损坏时不再丢弃或直接报错。结尾多余逗号、值内部裸引号、缺少闭合括号、对象后面跟了一段解释文字，都会先修复再校验，覆盖 `/v1/responses`、`/v1/messages` 和 OpenAI 兼容路径。
- 流式拼接不再被上游的坏习惯整坏：上游每帧重发完整参数缓冲、省略 `index`、所有调用都写 `index:0`、或者该给字符串却给了 JSON 对象，以前会拼出 `{"a":1}{"a":1}` 或静默丢片段，客户端那边就报「arguments are invalid JSON」。
- 真被输出 token 上限截断在字符串中间的参数仍然报错（不会补成看起来合法、实际却不是模型本意的命令），并在错误里说明是触到 token 上限。
