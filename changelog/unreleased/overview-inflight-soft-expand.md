### English

- 1. Overview InflightLiveCard first expand (and re-expand) uses a short body fade instead of staggering every wait pill / slot / fill / outcome — waits for the first live projection (or idle/mock board) so hydrate never fires a handoff storm; later live deltas keep queue→slot / Done·Failed / FLIP motion.
- 2. Mock「示意」demo is off by default when `/api/live` is unavailable (calm idle zeros, no raf). A header switch turns it back on; preference persists in `localStorage`.

### 中文

- 1. Overview「实时进行中」首次展开（及收起再开）改为短促整体淡入，不再对每个排队 pill / 槽位 / 填充 / 完成失败做 stagger；等首帧 live 投影（或空闲/示意板）就绪后再淡入，避免把 hydrate 当成一串交接飞入；之后的 live 增量仍保留排队→槽位 / 完成·失败 / FLIP。
- 2. 无 `/api/live` 时默认关闭 mock「示意」（空闲零值、无 raf）。卡片标题旁开关可再打开；偏好写入 `localStorage`。
