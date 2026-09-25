### English

- Add an experimental **Command Code** (`commandcode.ai`) provider that speaks the CLI's own `/alpha/generate` protocol. That endpoint is not plan-gated, so **every plan works — including the $1 Go plan**, whose Pro-gated `/provider/v1/*` generation endpoints return `403 upgrade_required`. The adapter serves the whole model catalog. Auth is a single `user_…` key, pasted through the existing PAT tab; the model list is read from the anonymous `/provider/v1/models`, and usage is shown from `/alpha/billing/credits` (monthly plan window plus the rolling 5-hour and weekly windows). Streaming is rewritten from the upstream newline-delimited JSON into OpenAI SSE. Experimental: `/alpha/generate` is unpublished and version-coupled, so the pinned CLI version fails loudly rather than degrading silently, and the provider is not production-ready.

### 中文

- 新增实验性 **Command Code**（`commandcode.ai`）渠道，走 CLI 自身的 `/alpha/generate` 协议。该端点不受档位限制，**所有套餐均可用 —— 包括 $1 Go 套餐**（其 Pro 专属的 `/provider/v1/*` 生成端点会返回 `403 upgrade_required`）。适配层覆盖整个模型目录。认证只需一把 `user_…` 密钥，经现有 PAT 页签粘贴；模型列表读取匿名的 `/provider/v1/models`；用量从 `/alpha/billing/credits` 展示（月套餐窗口，以及滚动的 5 小时 / 周窗口）。流式响应由上游的换行分隔 JSON 改写为 OpenAI SSE。实验性：`/alpha/generate` 未公开且与 CLI 版本耦合，故钉版不匹配时明确报错而非静默降级，不承诺生产可用。
