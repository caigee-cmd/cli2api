### English

- Overview InflightLiveCard slot→Done/Failed fly-out now scales down toward the outcome control (shrink in flight) instead of staying near full slot size.
- Overview InflightLiveCard waiting rail keeps a selecting (选号) request visible on `/api/live` `queue` for at least 500ms after Start even after Bind — display-only; Acquire / pickNext are not delayed.

### 中文

- Overview「实时进行中」槽位→完成/失败飞出动画改为飞行中缩小落向结局按钮，不再接近槽位原尺寸显得过大。
- Overview「实时进行中」等待队列在 Start 后至少保留 500ms 选号投影（即使已 Bind），仅影响 `/api/live` `queue` 展示，不拖延 Acquire / pickNext。
