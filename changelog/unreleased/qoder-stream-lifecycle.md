### English

- Qoder responses now finish as soon as the upstream sends `[DONE]`, even if the connection stays open.
- Closing a Qoder streaming response early now releases the upstream connection and its converter goroutine.

### 中文

- 上游发送 `[DONE]` 后 Qoder 响应立即结束，即使连接仍保持打开。
- 提前关闭 Qoder 流式响应时，现在会释放上游连接和转换协程。
