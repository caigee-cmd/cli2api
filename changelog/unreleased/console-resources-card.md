### English

- The overview page now shows a Resources card with live memory (RSS) and CPU usage for the server process and each managed worker, refreshed every 5 seconds, plus a new `GET /api/system/resources` endpoint.
- Bounded several unbounded memory paths: non-stream worker responses are capped at 64 MiB, inbound `/v1` request bodies at 64 MiB and console bodies at 1 MiB, per-worker log line buffering at 256 KiB, and the stats cache now evicts entries. Worker SSE parsing no longer retains every frame and builds large strings incrementally instead of repeated concatenation.

### 中文

- 概览页新增「资源占用」卡片，每 5 秒刷新展示主进程和各 worker 进程的内存（RSS）与 CPU 占用，并新增 `GET /api/system/resources` 接口。
- 收紧了多处内存上限：非流式 worker 响应限制 64 MiB，`/v1` 入站请求体限制 64 MiB、console 请求体限制 1 MiB，单条 worker 日志行缓冲限制 256 KiB，统计缓存加入淘汰；worker SSE 解析不再保留全部帧，大字符串改为增量拼接。
