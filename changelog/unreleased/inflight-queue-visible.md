### English

- 1. Overview InflightLiveCard selecting (选号) wait rail is visible again: expanded `/api/live` polls at 400ms, 1.6s display dwell (Acquire still immediate), and queue→slot fly-in uses GSAP MotionPath into the bound concurrency cell; cell model/elapsed stays hidden until the fly lands (or a short grace if skipped).
- 2. `/api/live` adds `occupancy` from BoundRequests: each in-flight request maps to its own concurrency cell (model + Bind elapsed). Finishing cells fly to Done/Failed from their own rect (`recent_completions.request_id`), staggered when several finish together — not one shared account animation.
- 3. Worker rail and wait rail use GSAP FLIP for yield/reorder: busy rises with a drag-up feel; remaining wait pills slide up into vacated gaps without fighting the next queue→slot fly.

### 中文

- 1. Overview「实时进行中」选号/排队重新可见：展开后 `/api/live` 400ms 轮询、选号展示约 1.6s（Acquire 不拖延）；排队→槽位用 GSAP 弧线滑入 Bind 并发格，模型/计秒等飞入落地后再显示（飞不起来则短 grace）。
- 2. `/api/live` 增加 `occupancy`（BoundRequests）：每个在途请求占一个并发格（各格模型与 Bind 秒数）；结束时各格从自身飞向完成/失败（`recent_completions.request_id`），同帧多格会错开——不是整账号共用一个飞出动画。
- 3. Worker 轨与等待轨用 GSAP FLIP 避让上顶：busy 上浮、空闲下沉；选号 pill 飞走后剩余排队上滑补位，不和下一次飞入打架。
