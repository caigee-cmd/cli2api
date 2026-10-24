### English

- Codex outer / CLI OAuth now proactively refreshes with a 96-hour lead (aligned with CLIProxyAPI `CodexOuterRefreshLead`), while Sign in with ChatGPT (SIWC) keeps the short 5-minute lead—so the nominal 10-day JWT is renewed before early upstream invalidation without changing SIWC behavior.
- Codex model catalog adds `gpt-6.1-sol` (GPT-6.1-Sol) from the upstream client model list, with the same slim fields as other `gpt-*` entries.

### 中文

- Codex outer / CLI OAuth 主动刷新提前量对齐 CLIProxyAPI 的 `CodexOuterRefreshLead`（96 小时），Sign in with ChatGPT（SIWC）仍保持 5 分钟短 lead，以便在名义 10 天 JWT 被上游提前作废前续期，且不改变 SIWC 行为。
- Codex 模型目录新增上游客户端列表中的 `gpt-6.1-sol`（GPT-6.1-Sol），字段结构与其它 `gpt-*` 条目一致。
