# InflightLiveCard motion gaps — work notes
时间：2026-10-29 15:43 Asia/Shanghai · tip 7701907 · PR #435

## 改动量：**中**

不引入 Motion / framer-motion（项目无该 dep；已有 gsap ^3.15）。

## Reuse（已有）

| 已有 | 用途 |
|------|------|
| `flyGhost` + `rectCache` + `queueEase` 560ms | 队列→槽 / 槽→Done·Failed 飞牌 |
| GSAP + CustomEase `inflightQueue` (0.22,1,0.36,1) | 全部 layout / grow |
| SlotBar `data-slot-fill` clip-path grow | 落槽进度条入场（已有，补 land 时序） |
| `CountUp` (`overview/CountUp.tsx`，RAF ease) | 指标/Done·Failed 数字缓动 |
| `tickMockInflight` + solid-glass tokens | mock 环 + 表面规范（无 orange wash） |

## 缺口 → 做法

1. **Queue vacancy FLIP** — fly-out 后对剩余 `data-wait-id` 用 rectCache First→Last，GSAP x/y 560ms queueEase 合缝
2. **Worker receive/occupy on land** — fly 完成再淡入槽位 pill + 延迟 clip grow；落点轻微 brightness pulse
3. **priority glide-past** — mock 加 `priority?: 'high'|'normal'`；高优 enqueue 后插到队前，FLIP 滑过
4. **CountUp wire** — Metric / Outcome 接 CountUp（int / ms）；后缀 `/min`、`/slots` 静态
5. **chart continuous glide** — DepthChart 稳定 x 索引 + 更长线性 scroll 感；peak domain 缓变
6. **retry → queue** — mock 失败约一半同 id 回 queued（不 +failed）；flyGhost worker→queue + drop-in

## 不做

- 不加 Motion dep
- 不改 solid-glass / 不引入 chart-3 orange wash
- 不 merge / 不 release

## + AffectUI overflow UX（steering）

7. **QUEUE overflow** — 仅前 N=3 条 pill；其余按 kind 色小方点堆叠；队缩时点上滑晋升为 pill（FLIP + drop），可顶到最上 pill 位。去掉灰色 pad 虚点。
8. **WORKERS overflow** — 只展示前 ~4 个 busy/rateLimited 优先槽，不铺满全部账号；计数仍用 summary.busyWorkers/workerSlots。

改动量仍为 **中**（同 reuse 表；overflow 为渲染裁剪 + 点位 FLIP）。
