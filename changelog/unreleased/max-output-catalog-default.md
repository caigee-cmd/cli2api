### English

- A request that omits `max_tokens` / `max_output_tokens` now defaults to the max output the model advertises in the catalog instead of the upstream's own 4096 cap, on both the chat and native Responses paths; explicit client values still win.

### 中文

- 客户端不传 `max_tokens` / `max_output_tokens` 时，改为按模型在目录里声明的最大输出上限下发，不再落到上游默认的 4096；客户端显式传的值仍然优先，chat 与原生 Responses 两条路径一致。
