### English

- The display catalog now reloads in the background every 10 minutes by default instead of only when a client asks, so a model an upstream adds appears in `/v1/models` without a manual refresh. Set `CLI2API_CATALOG_REFRESH_INTERVAL` (Go duration such as `15m`, or plain minutes) to change the interval; `0` disables the loop.
- The last successful catalog is persisted in SQLite and restored on boot, and a failed catalog refresh now keeps serving the previous model list instead of returning an empty one.
- `GET /v1/models` reports `catalog.synced_at` and `catalog.stale` beside `data`; upstream error text stays on the console endpoint so a scoped API key cannot read another provider's failure.
- `GET /api/model-catalog` (console) returns the catalog sync state with per-provider last success time, last error, and model count; `POST /api/model-catalog` forces a refresh.

### 中文

- 模型目录改为后台自动刷新，默认每 10 分钟一次，不再只在客户端请求时才拉取，上游新增的模型无需手动刷新就会出现在 `/v1/models`。刷新间隔用 `CLI2API_CATALOG_REFRESH_INTERVAL` 调整（Go duration 如 `15m`，或纯数字表示分钟），设为 `0` 关闭定时刷新。
- 最近一次成功的模型目录会写入 SQLite 并在启动时恢复；目录刷新失败时继续使用上一次的有效列表，不再返回空列表。
- `GET /v1/models` 在 `data` 旁边新增 `catalog.synced_at` 与 `catalog.stale`；上游错误文本只留在控制台接口，限定供应商的 API Key 读不到其他供应商的失败原因。
- 控制台 `GET /api/model-catalog` 返回目录同步状态（每个供应商的上次成功时间、失败原因、模型数量），`POST /api/model-catalog` 立即刷新一次。
