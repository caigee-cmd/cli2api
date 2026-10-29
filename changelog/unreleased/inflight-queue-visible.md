### English

- Overview InflightLiveCard selecting (选号) wait rail is visible again: expanded `/api/live` poll speeds up to 400ms, selecting display dwell is 1.6s (Acquire still immediate), and bound-within-dwell queue items keep `account_id` so pills can fly into the account slot.
- Live queue→slot fly-in no longer depends on matching mock job ids — leaving the wait rail targets the bind account (or a single newly busy slot). GSAP MotionPath gives a horizontal arc **slide into the concurrency cell** (not a teleport).
- `/api/live` adds `occupancy` from recorder BoundRequests: each in-flight request maps to a concurrency cell with its own model name and Bind `started_at`. Slot bar shows model + elapsed **per cell**, not one timer for the whole account.
- **Per-cell outcome fly (t240u):** when one account runs concurrency N (e.g. 3), each finishing cell flies to Done/Failed on its own from that cell’s rect (`recent_completions.request_id`). Same-tick multi-cell finishes stagger slightly — not one shared account animation.
- **Worker rail yield reorder (t241u):** when busy rises / idle drops, GSAP FLIP slides rows with a drag-up / 避让上顶 feel (riser leads + slight scale; yielders lag). New visible rows slide in; no hard snap.

### 中文

- Overview「实时进行中」选号/排队重新可见：面板展开时 `/api/live` 轮询加快到 400ms，选号展示保留约 1.6s（Acquire 仍不拖延），dwell 内已 Bind 的队列项保留 `account_id`，便于飞入账号槽位。
- 排队→槽位飞入不再依赖 mock 同 id；离开等待轨时按 Bind 账号（或唯一新忙槽）落点。GSAP MotionPath 做横向弧线 **滑入对应并发格子**（不是瞬移）。
- `/api/live` 增加 `occupancy`（recorder BoundRequests）：每个在途请求对应一个并发格，带模型名与 Bind `started_at`。槽位条按格子展示模型与秒数，**不再整账号共用一个计时**。
- **并发格各自飞出（t240u）：** 同一账号并发 N（如 3）时，每个格子结束都从该格飞向完成/失败（`recent_completions.request_id`）；同一帧多格结束会错开一点，**不是整账号共用一个飞出动画**。
- **Worker 轨避让上顶（t241u）：** busy 上浮 / 空闲下沉时 GSAP FLIP 滑动重排（升起的先行并略放大，避让行略延迟），新出现的行滑入，不用硬切。
