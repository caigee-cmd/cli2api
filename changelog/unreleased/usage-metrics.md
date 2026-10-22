### English

- **Console** · The overview traffic chart switches between requests, tokens (input / output / cache read) and latency (p95 / p50). The stats series now carries per-bucket tokens and p50/p95 latency.
- **Console** · New Usage page (sidebar → Usage) with sortable tables by model, account and provider: requests, success rate, p95, tokens, cache write and credits, using the same 1h / 24h / 7d / 30d / custom windows as the overview. Rows open the matching logs.
- **API** · `/api/logs/stats` accepts `dim=model,account,provider` and `limit` (default 6, max 500), and its grouped rows add p95 latency, token, cache and credit totals. Calls without these parameters behave as before. `/api/logs/requests` accepts `provider`, and the overview's provider rankings now drill down to the logs.

### 中文

- **界面** · 概览流量图可在请求、Tokens（输入 / 输出 / 缓存读取）与延迟（p95 / p50）之间切换；统计序列新增每个时间桶的 token 与 p50/p95 延迟。
- **界面** · 新增“用量”页面（侧边栏 → 用量），按模型、账号、供应商展示可排序的完整表格：请求数、成功率、p95、Tokens、缓存写入与积分；时间窗口与概览一致（1 小时 / 24 小时 / 7 天 / 30 天 / 自定义），点击行可查看对应日志。
- **API** · `/api/logs/stats` 支持 `dim=model,account,provider` 与 `limit`（默认 6，最大 500），分组结果新增 p95 延迟、token、缓存与积分合计；不带这些参数的请求行为不变。`/api/logs/requests` 支持 `provider` 筛选，概览的供应商排行也可下钻到日志。
