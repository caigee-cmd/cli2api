### English

- Non-streaming Qoder responses now keep the full tool call (ID, function name and all argument fragments) instead of only the last fragment.
- An empty `tool_calls` array from Qoder is no longer reported as a tool call or turned into a `tool_calls` finish.

### 中文

- Qoder 非流式响应现在保留完整的工具调用（ID、函数名和全部参数片段），不再只剩最后一个片段。
- Qoder 返回的空 `tool_calls` 数组不再被当作工具调用，也不再生成 `tool_calls` 结束原因。
