### English

- Force every model to use its own largest declared context window (1M where declared) for chat requests; remove the context/reasoning column from the model catalog and show the maximum window with declared reasoning strengths in model details. Reasoning levels now display and clamp from the model's resolved catalog only, with high tiers degrading to the nearest supported level.
- Align reasoning conversion with CC Switch conventions: accept `ultra` as the deepest tier and normalize Anthropic thinking budgets into the catalog-driven reasoning ladder.

### 中文

- 聊天请求强制使用每个模型自身声明的最大上下文窗口（声明了 1M 的就用 1M）；移除模型目录里的上下文/推理列，并在模型详情中展示最大上下文和该模型声明的思考强度。推理档位现在只按模型解析后的 catalog 展示和 clamp，高档位会降级到最接近的受支持档位。
- 推理转换对齐 CC Switch 约定：接受 `ultra` 作为最深档，并把 Anthropic thinking budget 归一化到由 catalog 驱动的推理档位。
