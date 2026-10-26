### English

- Codex streaming keeps the function name on tool calls: it is read from `response.output_item.added` instead of the argument events, which carry only the item id. Clients no longer receive a tool call whose `name` is empty, and a session that recorded one no longer has every later request rejected with `Invalid 'input[n].name': empty string`.

### 中文

- Codex 流式工具调用保留函数名：名字改从 `response.output_item.added` 的 item 读取，参数事件本身只带 item id。客户端不会再收到 `name` 为空的工具调用，之前记录过这种调用的会话也不会再被 `Invalid 'input[n].name': empty string` 整轮拒绝。
