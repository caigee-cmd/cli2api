### English

- Overview InflightLiveCard: richer busy-slot shimmer (brighter surface highlight + stronger chart-1 bands), opaque queue→slot fly with kind-tinted trail glow (~3 afterimages), fuller kind pastels / busy edge / soft chart-1 slot glow. Still project tokens only; no orange panel wash. Provider icons + short copy kept.
- Overview InflightLiveCard: busy→idle fly-outs route by per-job outcome — `/api/live` drains `recent_completions` from the request recorder (plus `last_error_kind` fallback). HTTP/API/upstream failures land on Failed (X), not Done (Check). Mock tick emits the same `completions` list so same-tick mixed finishes stay correct.

### 中文

- Overview「实时进行中」：忙碌槽流光提亮（更亮高光 + 更浓 chart-1 色带）；队列飞入槽位保持不透明胶囊 + kind 色拖尾光晕（约 3 道残影）；kind 粉彩与忙碌描边略加饱和，忙碌槽轻柔 chart-1 外辉。仍仅用项目 token，无大面积橙色洗底。ProviderMark 与精简文案保留。
- Overview「实时进行中」：busy→idle 飞出按单次请求结果路由——`/api/live` 从请求 recorder 抽干 `recent_completions`（并以 `last_error_kind` 兜底）。HTTP/API/上游失败落入 Failed（X），不再误进 Done（勾）。Mock 同 tick 也产出 `completions`，混合完成不再全进成功槽。
