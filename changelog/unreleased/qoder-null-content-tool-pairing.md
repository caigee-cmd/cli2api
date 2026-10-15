### English

- Fix Qoder chat rejecting multi-turn tool use with `400 InvalidParameter: messages with role "tool" must be a response to a preceeding message with "tool_calls"`. The upstream gateway only pairs a `role:"tool"` message with the assistant turn ahead of it when that assistant message's `content` is a string; OpenAI clients send `"content": null` for a pure tool-call turn, which left the call unregistered. The in-process client now rewrites a nil content on a tool-call turn to an empty string, which the gateway accepts. Call ids, tool results, and all other content shapes pass through untouched.

### 中文

- 修复 Qoder 多轮工具调用被上游以 `400 InvalidParameter: messages with role "tool" must be a response to a preceeding message with "tool_calls"` 拒绝的问题。上游网关只在前一条 assistant 消息的 `content` 为字符串时，才把它携带的 `tool_calls` 登记为可配对；OpenAI 客户端对纯工具调用轮次发送的正是 `"content": null`，导致调用未登记、后续 `role:"tool"` 消息被拒。进程内直连客户端现在会把工具调用轮次的 null content 改写为空字符串（网关接受该形状）；工具调用 id、工具结果与其他内容形状均原样保留。
