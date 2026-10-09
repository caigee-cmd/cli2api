### English

- Fix Qoder chat silently returning an empty, spec-invalid completion when the upstream gateway answers 200 with heartbeat frames only. The in-process client now rejects the empty stream before relaying (so the executor can cool the account down and fail over), salvages streams that were cut before the terminal `finish_reason` with a synthesized `stop` / `tool_calls` terminal chunk, and rejects empty non-stream responses instead of fabricating `stop`. Strict OpenAI-compatible clients no longer fail with `Stream ended without finish_reason`.

### 中文

- 修复 Qoder 上游网关偶发 200 + 仅心跳帧时，网关静默返回"空且不合规"的补全。进程内直连客户端现在会在转发前拒绝空流（执行层可冷却该账号并切换到其他账号重试）；对已输出内容但在终态 `finish_reason` 前被切断的流，合成 `stop` / `tool_calls` 终态帧进行挽救；非流式空响应不再凭空合成 `stop` 而是报错。严格的 OpenAI 兼容客户端（如 pi coding agent）不再出现 `Stream ended without finish_reason`。
