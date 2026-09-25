### English

- Show Qoder model pricing on the account's model list. Qoder reports a per-model `price_factor` (the multiplier its own client renders as `0.50x Credit`) plus `is_free`/`tags`, but the worker dropped them, so the console showed no price for any Qoder account. The worker now forwards them and the adapter renders the multiplier as the console's credits text. The free badge follows the Qoder client's own rule — the `limited_time_free` tag (or a zero factor) is free, a positive factor is priced — so a model that reports `is_free` alongside a real multiplier (Qwen3.8-Max: `is_free` + `0.5`) shows its multiplier instead of the contradictory "免费 / x0.5" pair. Applies to Qoder Global and Qoder CN.

### 中文

- 账号的模型列表现在会显示 Qoder 模型价格。Qoder 每个模型都带 `price_factor`（其客户端显示为 `0.50x Credit` 的那个倍率）以及 `is_free`/`tags`，但 worker 之前把它们丢掉了，所以控制台对所有 Qoder 账号都不显示价格。现在 worker 透传这些字段，适配层把倍率渲染为控制台的额度文案。免费标记对齐 Qoder 客户端自身的规则——带 `limited_time_free` 标签（或倍率为 0）才算免费，正倍率即视为计费——因此像 Qwen3.8-Max 这种「`is_free` 为真但同时带 0.5 倍率」的模型会显示倍率，而不再出现自相矛盾的「免费 / x0.5」。Qoder 国际版与国内版均适用。
