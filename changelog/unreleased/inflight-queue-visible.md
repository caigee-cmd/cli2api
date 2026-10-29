### English

- Overview InflightLiveCard selecting (选号) wait rail is visible again: expanded `/api/live` poll speeds up to 400ms, selecting display dwell is 1.6s (Acquire still immediate), and bound-within-dwell queue items keep `account_id` so pills can fly into the account slot.
- Live queue→slot fly-in no longer depends on matching mock job ids — leaving the wait rail targets the bind account (or a single newly busy slot). GSAP MotionPath gives a horizontal arc **slide into the concurrency cell** (not a teleport).
- `/api/live` adds `occupancy` from recorder BoundRequests: each in-flight request maps to a concurrency cell with its own model name and Bind `started_at`. Slot bar shows model + elapsed **per cell**, not one timer for the whole account.
- **Per-cell outcome fly (t240u):** when one account runs concurrency N (e.g. 3), each finishing cell flies to Done/Failed on its own from that cell’s rect (`recent_completions.request_id`). Same-tick multi-cell finishes stagger slightly — not one shared account animation.
- **Worker rail yield reorder (t241u):** when busy rises / idle drops, GSAP FLIP slides rows with a drag-up / 避让上顶 feel (riser leads + slight scale; yielders lag). New visible rows slide in; no hard snap.
- **Land-gated cell chrome (t243u):** occupied cell model/elapsed stays hidden until the queue→slot fly *lands* (or a short grace if the fly is skipped). Dwell-bound occupancy keeps a fly-target cell with `pendingInbound` — no “slot already full while ghost still flying”. Timings unchanged: selectingMinDwell 1.6s, expanded poll 400ms, fly 0.88s; Acquire still immediate.
- **Wait-rail vacancy yield (t244u):** when a selecting pill flies into a slot, remaining wait pills/dots FLIP/slide up into the gap (0.56s). Vacancy FLIP is interrupt-safe (kill + visual→layout) and refreshes wait `rectCache` onUpdate, so another item that finishes selecting mid-FLIP still gets a correct queue→slot fly — no fighting transforms.

### 中文

- Overview「实时进行中」选号/排队重新可见：面板展开时 `/api/live` 轮询加快到 400ms，选号展示保留约 1.6s（Acquire 仍不拖延），dwell 内已 Bind 的队列项保留 `account_id`，便于飞入账号槽位。
- 排队→槽位飞入不再依赖 mock 同 id；离开等待轨时按 Bind 账号（或唯一新忙槽）落点。GSAP MotionPath 做横向弧线 **滑入对应并发格子**（不是瞬移）。
- `/api/live` 增加 `occupancy`（recorder BoundRequests）：每个在途请求对应一个并发格，带模型名与 Bind `started_at`。槽位条按格子展示模型与秒数，**不再整账号共用一个计时**。
- **并发格各自飞出（t240u）：** 同一账号并发 N（如 3）时，每个格子结束都从该格飞向完成/失败（`recent_completions.request_id`）；同一帧多格结束会错开一点，**不是整账号共用一个飞出动画**。
- **Worker 轨避让上顶（t241u）：** busy 上浮 / 空闲下沉时 GSAP FLIP 滑动重排（升起的先行并略放大，避让行略延迟），新出现的行滑入，不用硬切。
- **落点后再亮格（t243u）：** 并发格的模型/计秒要等排队→槽位飞入 *落地* 才显示（飞不起来则短 grace）。dwell 内已 Bind 的格子只占位（`pendingInbound`），避免「鬼影还在飞、槽已占满」。时间不变：选号 dwell 1.6s、展开轮询 400ms、飞入 0.88s；Acquire 仍不拖延。
- **等待轨避让上顶（t244u）：** 选号 pill 飞入槽位后，剩下的排队 pill/点 FLIP 上滑补位（0.56s）。空位 FLIP 可打断（kill + visual→layout），onUpdate 刷新 wait 矩形缓存，所以上滑过程中又有一项选号结束仍能正确飞入槽位，不和位移动画打架。
