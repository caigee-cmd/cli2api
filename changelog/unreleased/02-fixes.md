### English

- **Fix** · `/v1/messages` accepts in-band `role: "system"` messages; Claude Code 2.1.293+ no longer gets 400. (#328)
- **Fix** · Codex Responses-Lite models (e.g. GPT-5.6) send `reasoning.context: "all_turns"`, fixing upstream 400. (#341)
- **Fix** · WorkBuddy keeps `max` reasoning effort when the catalog advertises it, instead of downgrading to `xhigh`. (#343)
- **Fix** · Qoder streams: unique completion `id` per request, finish on `[DONE]`, release the upstream on early close, and fail over heartbeat-only upstreams (`upstream_empty`). (#339, #342, #345)
- **Fix** · Qoder tool calls: non-streaming keeps the full call; an empty `tool_calls` array is ignored. (#338)
- **Fix** · Qoder keeps earlier assistant `reasoning_content`; model key mappings refresh with each catalog refresh. (#340, #346)
- **Fix** · Legacy Qoder accounts (pre-v0.7.0 CLI login, no PAT) fall back to the worker again; no more "account not found". (#337)
- **Fix** · Qoder check-in shows why nothing was claimable; token renewal no longer clears held tokens. (#327)

### 中文

- **修复** · `/v1/messages` 支持 `messages` 内的 `role: "system"`，Claude Code 2.1.293+ 不再报 400。（#328）
- **修复** · Codex Responses-Lite 模型（如 GPT-5.6）发送 `reasoning.context: "all_turns"`，修复上游 400。（#341）
- **修复** · WorkBuddy 在目录支持 `max` 时不再把推理档位降级为 `xhigh`。（#343）
- **修复** · Qoder 流式：每请求唯一 completion `id`、收到 `[DONE]` 即结束、提前关闭时释放上游、仅心跳的上游以 `upstream_empty` 切换。（#339、#342、#345）
- **修复** · Qoder 工具调用：非流式保留完整调用；空 `tool_calls` 数组不再视为工具调用。（#338）
- **修复** · Qoder 保留历史 assistant 的 `reasoning_content`；模型 key 映射随目录刷新替换。（#340、#346）
- **修复** · v0.7.0 前 CLI 登录、无 PAT 的老 Qoder 账号重新回退 worker，不再报 "account not found"。（#337）
- **修复** · Qoder 签到无可领活动时显示原因；token 续期缺字段时保留原值。（#327）
