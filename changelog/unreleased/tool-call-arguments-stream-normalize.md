### English

- Streaming tool calls now deliver arguments as one already-normalized string instead of relaying upstream fragments. `/v1/responses` sends the repaired arguments once in `response.function_call_arguments.delta` and repeats them in `.done`, `/v1/messages` sends them at block close, and `/v1/chat/completions` withholds tool frames until the call is complete. Clients that concatenate deltas and clients that read the done event therefore receive identical, valid JSON.
- Arguments truncated mid-string are no longer padded. The unfinished trailing member is dropped, so the call still parses and the tool or model can recover, instead of the round failing with "arguments are invalid JSON".

### 中文

- 流式工具调用不再把上游碎片直接转发，而是一次性下发已经归一化好的参数：`/v1/responses` 在 `response.function_call_arguments.delta` 给出修复后的完整参数并在 `.done` 重复一次，`/v1/messages` 在 block 收尾时发，`/v1/chat/completions` 把工具帧拦到调用结束再发。因此「拼接 delta 的客户端」和「读 done 事件的客户端」拿到的是同一份合法 JSON。
- 截断在字符串中间的参数不再补齐：只丢弃尾部未完成的那个成员，调用仍然能解析，交给工具或模型自己纠正，而不是整轮报「arguments are invalid JSON」。
