### English

- Overview InflightLiveCard selecting (选号) wait rail is visible again: expanded `/api/live` poll speeds up to 400ms, selecting display dwell is 1.6s (Acquire still immediate), and bound-within-dwell queue items keep `account_id` so pills can fly into the account slot.
- Live queue→slot fly-in no longer depends on matching mock job ids — leaving the wait rail targets the bind account (or a single newly busy slot). GSAP MotionPath gives a horizontal arc **slide into the concurrency cell** (not a teleport).
- `/api/live` adds `occupancy` from recorder BoundRequests: each in-flight request maps to a concurrency cell with its own model name and Bind `started_at`. Slot bar shows model + elapsed **per cell**, not one timer for the whole account.

### 中文

- Overview「实时进行中」选号/排队重新可见：面板展开时 `/api/live` 轮询加快到 400ms，选号展示保留约 1.6s（Acquire 仍不拖延），dwell 内已 Bind 的队列项保留 `account_id`，便于飞入账号槽位。
- 排队→槽位飞入不再依赖 mock 同 id；离开等待轨时按 Bind 账号（或唯一新忙槽）落点。GSAP MotionPath 做横向弧线 **滑入对应并发格子**（不是瞬移）。
- `/api/live` 增加 `occupancy`（recorder BoundRequests）：每个在途请求对应一个并发格，带模型名与 Bind `started_at`。槽位条按格子展示模型与秒数，**不再整账号共用一个计时**。
