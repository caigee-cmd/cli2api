### English

- Request-log retention no longer deletes the only copy of the usage history. `request_logs` is both the per-request detail log and the table every aggregate reads (Usage page, Cost panel, traffic chart), so the old single age-based `DELETE` erased the totals for a day at the same moment it erased that day's detail — and there is no rollup table to reconstruct them. Retention is now two stages: past `CLI2API_LOG_RETENTION_DAYS` a row keeps every column the aggregates touch (status, stream, model, account, API key, provider, tokens, cache, credits, latency, TTFB, `error_kind`) and loses only what the detail drill-down reads (child rows, error text, message roles, reasoning, routing, `finished_at`); past `CLI2API_LOG_TOTALS_RETENTION_DAYS` (default 730) the row is deleted. The Usage and Cost numbers therefore stay exact rather than approximated, and the file still stops growing because the child tables were ~90% of its bytes.
- `CLI2API_LOG_MAX_ROWS` now counts only rows that still carry detail. Totals-only rows are not subject to it, so raising the retention window is no longer silently undone by the row cap deleting the oldest rows first.
- The Usage **Cost** panel window follows `CLI2API_LOG_TOTALS_RETENTION_DAYS` instead of the detail window, so a raised retention is actually visible there.
- `GET /api/logs/requests` and `/api/logs/requests/<id>` gain `detail_expired`, so a console can tell "this request has no attempts recorded" apart from "this request's detail was cleared by retention".

### 中文

- 请求日志保留不再删掉用量历史的唯一副本。`request_logs` 既是逐请求明细，也是所有聚合查询（用量页、费用面板、流量图）读取的表，原来的单段按龄 `DELETE` 在删掉某天明细的同时把那天的汇总一起抹掉，而库里没有任何汇总表可以重建。现在保留分两段：超过 `CLI2API_LOG_RETENTION_DAYS` 的行保留全部聚合列（状态、stream、模型、账号、API key、渠道、tokens、缓存、积分、延迟、TTFB、`error_kind`），只清掉明细视图才读的内容（子表行、错误文本、消息角色、推理等级、routing、`finished_at`）；超过 `CLI2API_LOG_TOTALS_RETENTION_DAYS`（默认 730 天）才删除整行。因此用量与费用数字保持精确而非近似，文件也不会无限增长——原先子表占了约九成体积。
- `CLI2API_LOG_MAX_ROWS` 现在只统计仍带明细的行。只剩汇总的行不计入，所以调大保留期不会再被行数上限按「最老的先删」悄悄抵消。
- 用量页「费用」选项卡的时间范围改为跟随 `CLI2API_LOG_TOTALS_RETENTION_DAYS`，而不是明细保留期，调大保留期后该面板才真正看得到更早的历史。
- `GET /api/logs/requests` 与 `/api/logs/requests/<id>` 新增 `detail_expired` 字段，控制台可以区分「这次请求本来就没记录 attempt」和「这次请求的明细已被保留策略清理」。
