### English

- Current concurrency now tracks live proxy requests (Acquire/Release on the executor). `/api/live` and the sidebar update within a few seconds; health probes no longer overwrite in-flight counts.

### 中文

- 「当前并发」改为按代理正在处理的请求实时加减；`/api/live` 与侧边栏几秒内跟上，健康探活不再覆盖进行中的并发数。
