### English

- API-key lookups are cached for 30 seconds (configurable with `CLI2API_AUTH_CACHE_MS`, `0` disables); console key edits and deletions invalidate the cache immediately, and `last_used_at` is written at most once per minute instead of on every request.
- Upstream HTTP keeps a 100-connection-per-host idle pool and bounds the response-head wait at five minutes, so bursts reuse TLS connections and a silent upstream fails over instead of pinning a request forever.
- Shutdown now drains: `/ready` reports 503 and new work is rejected while active streams finish for up to 30 seconds; compose waits 40 seconds before SIGKILL, and `/health` exposes the draining state.
- Compose caps container logs at 10 MB × 3 files so a chatty or failing gateway cannot fill the host disk.

### 中文

- API Key 查询缓存 30 秒（可用 `CLI2API_AUTH_CACHE_MS` 调整，`0` 关闭）；控制台修改或删除 Key 会立即失效缓存，`last_used_at` 最多每分钟写一次，不再每请求写一次 SQLite。
- 上游 HTTP 连接池提升到每主机 100 个空闲连接，并把响应头等待上限设为 5 分钟；突发流量复用 TLS 连接，静默上游会触发切换而不是永久占住请求。
- 关闭服务时先进入 draining：`/ready` 返回 503、新请求被拒绝，已有流最多 30 秒完成；compose 等待 40 秒再发 SIGKILL，`/health` 同时暴露 draining 状态。
- compose 限制容器日志为 10 MB × 3 份，避免异常日志把宿主机磁盘写满。
