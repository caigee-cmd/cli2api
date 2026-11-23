### English

- Align reasoning conversion with CC Switch: accept `ultra` as the deepest tier and normalize Anthropic `thinking.budget_tokens` (2048 / 8192 / 16384 / 24576) onto the catalog-driven reasoning ladder.
- Unify provider reasoning intent parsing behind one shared parser while keeping each provider's legacy wire behavior: explicit `thinking:{"type":"disabled"}` stays a separate off switch, and effort strings, effort objects, and enable-thinking flags resolve as before.
- Canonicalize protocol usage across Anthropic, OpenAI chat, and Responses streams: `input_tokens` now excludes cache read/write, cache buckets are always reported, and usage fields from provider aliases (`cache_read_input_tokens`, `cached_tokens`, `cache_creation_input_tokens`) are folded into one shape.
- Keep Trae max mode client-owned: only an explicit `is_max_mode` request is forwarded, never inferred from the catalog or a stored console setting.

### 中文

- 推理转换对齐 CC Switch 约定：接受 `ultra` 作为最深档，并把 Anthropic `thinking.budget_tokens`（2048 / 8192 / 16384 / 24576）归一化到由 catalog 驱动的推理档位。
- 用统一的解析器处理各供应商的推理意图，同时保持各自的旧有请求行为：`thinking:{"type":"disabled"}` 仍是独立的关闭开关，effort 字符串、effort 对象和 enable-thinking 开关的解析结果不变。
- 统一 Anthropic、OpenAI chat 和 Responses 流的 usage 口径：`input_tokens` 不再包含缓存读写，缓存字段始终上报，并把供应商别名（`cache_read_input_tokens`、`cached_tokens`、`cache_creation_input_tokens`）折算为同一形状。
- Trae 的 Max 模式保持由客户端决定：只有请求显式带上 `is_max_mode` 才透传，不再从 catalog 或控制台保存的设置推断。
