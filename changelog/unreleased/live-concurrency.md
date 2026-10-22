### English

- Overview is denser (tighter card padding, gaps, metric size and chart height) and opens with a slim one-row live readout: current concurrency against total capacity (sum of `max_inflight` over enabled accounts), requests started in the last 60 seconds, and a 60-second concurrency trace. It polls the new lightweight `GET /api/live` every 2.5s, only while the tab is visible.
- The overview account pool shows a thin `in_flight / max_inflight` bar per account; saturated accounts are marked in a warning tone.

### 中文

- 概览页整体更紧凑（卡片内边距、间距、指标字号与图表高度下调），顶部新增单行实时读数：当前并发 / 总容量（已启用账号 `max_inflight` 之和）、近 60 秒请求数，以及近 60 秒并发走势。通过新增的轻量接口 `GET /api/live` 每 2.5 秒轮询，仅在页面可见时轮询。
- 概览账号池为每个账号显示 `in_flight / max_inflight` 细条，满载账号以警示色标出。
