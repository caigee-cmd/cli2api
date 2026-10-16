### English

- Preserve and normalize prompt cache token metrics (\cache_read_tokens\, \prompt_cache_hit_tokens\, and \prompt_tokens_details\) across Trae, Qoder, and WorkBuddy providers in both streaming and non-streaming responses.
- Gracefully salvage upstream streams that finish with a terminal \inish_reason\ before EOF without emitting literal \[DONE]\, preventing artificial 502 incomplete stream errors.

### 中文

- 修复并标准化 Trae、Qoder 与 WorkBuddy 提供商在流式与非流式响应中的 Prompt 缓存 Token 指标（统一解析并透传 \cache_read_tokens\、\prompt_cache_hit_tokens\ 与 \prompt_tokens_details\）。
- 容错处理上游流式响应在带有终态 \inish_reason\ 正常结束但未显式发送 \[DONE]\ 的边界情况，自动补齐结束帧，避免下游误判为 502 流异常中断。
