### English

- 1. Overview InflightLiveCard slot→Done/Failed fly-out now scales down toward the outcome control (shrink in flight) instead of staying near full slot size; after the hairline flash the Done/Failed pill does a short left-right wiggle + scale (stuffed-in feel).
- 2. Overview InflightLiveCard waiting rail keeps a selecting (选号) request visible on `/api/live` `queue` for at least 500ms after Start even after Bind — display-only; Acquire / pickNext are not delayed.
- 3. Overview InflightLiveCard Done/Failed are session counters while the card is expanded (bumped from live completions), not historical totals; rate-limit remaining chips, depth band labels, and cooldown pause styling are removed so the card stays in-progress only. Outcome sinks use a clearer icon chip + label.

### 中文

- 1. Overview「实时进行中」槽位→完成/失败飞出动画改为飞行中缩小落向结局按钮；落地描边闪一下后，完成/失败块再左右轻晃并缩放一点，像被塞进去。
- 2. Overview「实时进行中」等待队列在 Start 后至少保留 500ms 选号投影（即使已 Bind），仅影响 `/api/live` `queue` 展示，不拖延 Acquire / pickNext。
- 3. Overview「实时进行中」完成/失败改为展开会话内计数（随 live 完成事件累加），不是历史总量；去掉顶部限流剩余、深度图限流色带标签与冷却暂停样式，卡片只展示进行中；结局块图标与标签更清晰。
