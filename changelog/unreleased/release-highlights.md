### English

- 1. Overview InflightLiveCard waiting rail now shows real selecting (选号) requests from `/api/live` `queue` (Prepare → pool Acquire), reusing existing `request_logs` statuses (`started` / `streaming` / terminal) instead of a parallel inflight API.
- 2. Worker rail order is busy → available → cooldown, and rate-limit remaining time formats with hours when the window is long enough (`Nh Nm`).

### 中文

- 1. Overview「实时进行中」等待队列改为读取 `/api/live` 的 `queue`（Prepare → 选号 → Acquire），复用现有 `request_logs` 状态（`started` / `streaming` / 终态），不另开 `/api/inflight*`。
- 2. 账号槽排序为忙碌 → 空闲 → 冷却，限流剩余时间在足够长时按小时显示（`Nh Nm`）。
