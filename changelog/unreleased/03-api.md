### English

- **API** · New `GET /api/live` for live concurrency. `/api/logs/stats` accepts `dim=model,account,provider` and `limit`, with p95, token, cache and credit totals; `/api/logs/requests` accepts `provider`. Existing calls behave as before.

### 中文

- **API** · 新增 `GET /api/live` 实时并发接口；`/api/logs/stats` 支持 `dim=model,account,provider` 与 `limit`，返回 p95、Token、缓存与积分合计；`/api/logs/requests` 支持 `provider` 筛选。原有调用行为不变。
