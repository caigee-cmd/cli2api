### English

- Qoder recovers fake-XML `<tool_call>…</tool_call>` content echoes into structured `tool_calls` (hub #140 / e6e7622), including stream hold, and repairs mangled tool-argument JSON before fail-closed (hub 70ba58b args path only).
- Qoder treats provider `Image conversion timeout` as a same-account, one-shot retry without cooling or rotating the account (hub v1.8.0 / cdd95ef); after the bounded retry the executor still does not fail over.

### 中文

- Qoder 将伪 XML `<tool_call>…</tool_call>` 正文回声还原为结构化 `tool_calls`（对齐 hub #140 / e6e7622，含流式暂存），并在 fail-closed 前尽量修复损坏的工具参数 JSON（只跟 hub 70ba58b 的坏参数修复，不含首字 504 换号）。
- Qoder 对上游 `Image conversion timeout` 在同账号做一次有界重试，不冷却、不换号（对齐 hub v1.8.0 / cdd95ef）；有界重试用尽后执行层仍不因此 fail over。
