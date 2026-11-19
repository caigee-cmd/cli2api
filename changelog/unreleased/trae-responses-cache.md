### English

- Fixed cache visibility for Trae/SOLO CN across Responses, Chat Completions, and Messages by recognizing additional upstream cache-token fields and emitting protocol-correct usage fields. Responses now carries cache write tokens, Anthropic responses subtract cache buckets from fresh `input_tokens`, and truncation/refusal/context-limit finishes map to explicit incomplete statuses.
- Aligned reasoning conversion with CC Switch conventions by accepting `ultra` as the deepest tier and normalizing Anthropic thinking budgets into the catalog-driven reasoning ladder.

### 中文

- 修复 Trae/SOLO 国内版在 Responses、Chat Completions 和 Messages 协议中的缓存可见性：识别更多上游缓存 token 字段，并按各协议正确输出 usage 字段。Responses 现在携带缓存写入 token，Anthropic 响应从 fresh `input_tokens` 中扣除缓存桶，截断/拒答/上下文超限会映射为明确的 incomplete 状态。
- 推理转换对齐 CC Switch 约定：接受 `ultra` 作为最深档，并把 Anthropic thinking budget 归一化到由 catalog 驱动的推理档位。
