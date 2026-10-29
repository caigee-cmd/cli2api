### English

- Overview adds a read-only「Live now」summary card (Phase 1 mock): Waiting → account-slot bars → Done/Failed, depth chart with amber rate-limit bands, GSAP/CSS motion aligned to **public** AffectUI Agent Queue demo timings; queue pills and account slots reuse project **solid-glass** (top-edge rim + light contact shadow, not frosted/gray wells); amber only for rate-limit/cooldown; no pause/cancel/concurrency; `/live` not built yet.
- `/api/live` extends with a read-only projection: `workers` from AccountView/Pool in-flight, `rate_limits` from DownUntil/ModelCooldowns, `sticky` from SessionAffinity.Stats — no new cooldown state machine, no `/api/inflight*`, no SQLite. Shape matches Stage-3 mock swap.

### 中文

- Overview 增加只读「实时进行中」摘要卡（阶段 1 示意）：等待 → 账号槽位进度条 → 完成/失败、带琥珀限流色带的深度图；等待队列项与账号槽位复用项目 **solid-glass**（顶边高光 + 轻阴影，非磨砂/灰底）；琥珀仅用于限流/冷却；无暂停/取消/并发调节；尚未做 `/live`。
- `/api/live` 扩展只读投影：`workers` 来自 AccountView/Pool 进行中、`rate_limits` 来自 DownUntil/ModelCooldowns、`sticky` 来自 SessionAffinity.Stats — 不新建冷却状态机、不新增 `/api/inflight*`、不碰 SQLite；字段形状对齐阶段 3 与 mock 互换。

- Stage-1 mock live loop: waiting pills get a subtle 流光; running bars keep stronger sheen+glow; jobs FLIP/fly queue→worker and worker→Done/Failed (AffectUI public Motion prose; GSAP + solid-glass ghosts). Depth series scrolls; rate-limit bars stay amber-frozen. Entrance timings unchanged (520/420/560ms, ease 0.22,1,0.36,1).
- 阶段 1 mock 活环：等待队列轻流光、进行中条更强扫光/前沿光晕；任务从队列飞入账号槽、完成后飞入 Done/Failed；深度图滚动；限流槽琥珀冻结。入场时长与公开 demo 一致。


- Motion gaps filled (GSAP only): queue vacancy FLIP after fly-out 560ms; worker receive/occupy on land; priority glide-past (mock `priority`); CountUp on metrics/Done·Failed; depth chart continuous glide + eased Y peak; retry slides back to queue when mock emits. AffectUI overflow: first 3 queue pills + kind-colored overflow dots (slide up on shrink); workers rail shows ≤4 busy/rate-limited slots. solid-glass kept; no chart-3 orange wash.
- 动效缺口补齐（仅 GSAP）：飞出后队列合缝 FLIP；落槽占用；高优插队滑过；指标 CountUp；深度图连续滑移；失败可重试滑回队列。溢出：队列仅前 3 条 pill + 色点堆；槽位最多 4 条 busy/限流。solid-glass 不变，无橙色洗底。
