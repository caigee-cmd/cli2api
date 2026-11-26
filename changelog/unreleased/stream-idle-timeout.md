### English

- Streaming responses now carry a 5-minute upstream idle deadline (`CLI2API_STREAM_IDLE_MS`, clamped to at least 30s). An upstream that answers the response head and then goes silent no longer holds the account's in-flight slot, the client connection and a permanently "streaming" request row: the relay reports `stream_idle_timeout`, the account cools down for 10s, and the slot is released.
- On boot, request rows a previous process left in `started`/`streaming` are closed as `error` / `stream_abandoned`, so the console stops showing phantom running requests after every restart.

### 中文

- 流式响应新增 5 分钟上游空闲上限（`CLI2API_STREAM_IDLE_MS`，最低按 30 秒）。上游回了响应头后长时间不吐字节时，不再永久占住账号并发槽位、客户端连接和一直显示 `streaming` 的请求记录：转发报 `stream_idle_timeout`，该账号冷却 10 秒并释放槽位。
- 启动时会把上一进程遗留的 `started`/`streaming` 请求记录收尾为 `error` / `stream_abandoned`，重启后控制台不再出现幽灵运行中的请求。
