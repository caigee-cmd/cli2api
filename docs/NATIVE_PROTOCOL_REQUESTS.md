---
id: cli2api-native-protocol-requests
title: 原生协议请求的现状与后续
scope: [gateway, executor, translate, providers]
status: current
read-when: 理解三个公共入口与上游协议的关系、改 Responses 原生链路或评估 ChatRequest 边界时
summary: 三个公共入口的当前转发方式。Responses 在 adapter 声明原生能力时保留原文；Chat 与 Messages 仍先译成 ChatRequest。Codex 两条路径共用同一套上游约束。
related: [AGENTS.md, docs/ARCHITECTURE_SUMMARY.md, docs/REFACTORING.md, docs/DEVELOPMENT.md]
last-updated: 2026-09-25
---

# 原生协议请求的现状与后续

## 1. 现在是什么

第一轮已经落地，而且只落地了 Responses。本文描述当前代码，不是待实施设计。

- `translate.ChatRequest` 仍是兼容表示，不再是所有请求的强制中间表示。
- 客户端入口决定返回协议，adapter 决定怎么访问上游。两边互不绑定。
- `/v1/responses` 先保留原文。候选账号的 adapter 实现了 `NativeResponsesStreamer` 时走原生路径；否则才在兼容转换能成功时走 `ChatRequest`。
- `/v1/chat/completions` 和 `/v1/messages` 没有原生路径。它们仍先变成 `ChatRequest`。选中 Codex 时，adapter 再把它建成 Responses 请求。
- 路由、权限、冷却、重试和日志仍在 executor。没有第二套直通执行器。
- 原生能力看接口，不看 provider 名字。gateway 里没有 `if provider == "codex"`。

「原生」指不经 Chat 重建，从而保住 item 顺序、id、`call_id`、namespace 和未知成员。它不是 HTTP body 的字节级复制。Codex 上游不接受的字段会按明确清单改写，见第 4 节。

## 2. 三个入口现在怎么转发

```text
/v1/messages            Messages → ChatRequest ─────────────┐
/v1/chat/completions    ChatRequest ────────────────────────┼─→ 选号
/v1/responses           保留原文 ──→ 有原生 adapter？         │
                              ├─ 是：原文副本 + 本次执行参数 ──┤
                              └─ 否：能转 Chat 才继续 ────────┘
                                                    │
                         选中的 adapter 决定上游协议
                         Codex：两条路径都打 Responses，并套同一套上游约束
                         其他：沿用各自的 Chat / 私有协议
                                                    │
                         返回格式仍由入口决定
                         Responses 原生结果：SSE 转发，或从终态事件收完整 JSON
                         其他：先回到内部兼容结果，再编码成入口协议
```

### 2.1 `/v1/responses`

`HandleResponses` 有上限地读完 body，解析为 `translate.NativeResponsesRequest`。非法 JSON、非对象、尾随值、缺 `model` 或 `input`、以及 `previous_response_id` / `conversation` 直接拒绝。

兼容转换 `Compat()` 会试一次，但失败不再挡住请求。随后：

1. 有实现 `NativeResponsesStreamer` 的候选账号时，只在这些账号里选。每次尝试从原文取一份副本，带上 `providers.RequestOptions`（本次上游模型、是否去掉 system/developer）。流式转发上游 SSE；`stream:false` 仍向上游要 SSE，再由 `translate.CollectResponses` 取出终态 `response` 对象。失败不降级到 Chat 重建。
2. 没有原生账号，且原文能转成 `ChatRequest` 时，走原来的 `ChatStream` / `ChatNonStream`，网关再把结果拼回 Responses。
3. 两边都不行，返回 `unsupported_request`。

现在只有 Codex 实现了这个接口。

### 2.2 `/v1/chat/completions` 和 `/v1/messages`

这两条没有原文保留，也没有原生分派。

- Messages 先经 `TranslateAnthropicMessages` 变成 `ChatRequest`。
- Chat 本身就是 `ChatRequest`。
- 选中 Codex 时，`buildBody` 把消息建成 Responses `input`，再进入第 4 节的上游改写。上游 SSE 由 Codex 译回 Chat 事件，gateway 按入口编码成 Chat Completions 或 Anthropic Messages。
- 选中其他 provider 时不经过 Responses。

非流式的兼容结果仍是 `ChatOutcome`。这只发生在非原生路径，以及 Chat/Messages 入口。

## 3. 请求和能力现在落在哪

| 内容 | 现在的位置 | 约束 |
| --- | --- | --- |
| Responses 原文 | `translate.NativeResponsesRequest` | 自有 body。`Fields()` 每次返回副本，failover 不能改到下一次 |
| 兼容表示 | `translate.ChatRequest` | Chat/Messages 的唯一内容；Responses 仅在没有原生账号且转换成功时使用 |
| 本次执行决定 | `providers.RequestOptions` | 显式参数，不放进 context。目前是上游模型 id 和 `DropSystemPrompt` |
| 原生能力 | `NativeResponsesStreamer.ResponsesStream` | 入参是原文指针和 `RequestOptions`，返回上游 Responses SSE |
| 事件判定 | `translate.ParseResponsesEvent` | relay 和 collector 共用。`response.failed` 是终态错误，不再补第二条错误帧 |
| 工具名还原 | `execution.ResponseToolNames` | 只在 gateway。兼容转换失败时从原文的 tools 声明补 |

`ProviderChat` 的 `ChatRequest` 签名没改。`context` 仍只带取消、超时和请求追踪，不带请求主体。

选号、权限、pin、粘性和日志仍在 `executor.Prepare` 和原来的尝试循环里。原生请求的 reasoning 取 `reasoning.effort`。会话种子优先用显式 header，其次用兼容转换算出的内容种子；只有兼容转换给不出种子时，才用原文里稳定的首个 user item。追加后续 item 不改变这个种子。

日志不会为了凑 `MessageCount` 去转换原文。兼容转换失败时，消息条数就是 0，不把 Responses item 数假装成 chat message 数。

## 4. Codex 上游约束

ChatGPT Codex 后端不是公开 Responses API 的超集。CLIProxyAPI 在每次 `/responses` 调用前做同一组改写；本仓库两条路径共用 `normalizeCodexUpstream`，所以原生转发和 Chat/Messages 翻译出去的形状一致。

这是明确清单，不是「未知字段一律保留」，也不是「未知字段一律删掉」。

| 字段 | 处理 |
| --- | --- |
| `model` | 换成这次尝试解析出的上游模型 |
| `stream` | 强制 `true`。客户端是否流式只决定 gateway 转发还是收集 |
| `store` | 强制 `false` |
| `include` | 强制 `["reasoning.encrypted_content"]` |
| `instructions` | 缺省补 `""`。账号策略要求去掉提示时覆盖调用方的值，并同时去掉 input 里的 system/developer 消息 |
| `input` | 字符串收成一条 user message。`system` 改成 `developer`。剥掉 `prompt_cache_breakpoint`。item 顺序、`call_id`、namespace 和未改写的成员保留 |
| item `id` | 补 `msg_` / `rs_` / `fc_` / `ctc_` / `ctco_` 前缀；超过 64 字符的截断。超长且带 `encrypted_content` 的 reasoning item 丢掉，因为截断会破坏回放 |
| reasoning item | `content` 清空，空 summary 时先把 `reasoning_text` 提升进去。没有 `encrypted_content` 的 id 去掉，避免 `store:false` 被当成查找 |
| `reasoning.effort` | 按 catalog clamp。其他 reasoning 成员保留 |
| `parallel_tool_calls` | 有工具时非 lite 强制 `true`；lite 模型强制 `false`。没有工具时非 lite 删掉这个字段 |
| `service_tier` | 只留 `priority` 和 `ultrafast`。`fast` 改成 `priority`，其他值删掉 |
| `tools` / `tool_choice` | `web_search_preview` 改成 `web_search`。schema 里去掉 `\p{}`、`\P{}`、`\0` 这类 pattern；足够大的纯常量 `oneOf` / `anyOf` 收成 `enum` |
| 后端直接拒绝的顶层字段 | 删掉 `max_output_tokens`、`max_completion_tokens`、`max_tokens`、`temperature`、`top_p`、`top_k`、`truncation`、`user`、`prompt_cache_options`、`prompt_cache_retention`、`safety_identifier`、`stream_options`、`previous_response_id`、`generate`、`context_management` |
| `prompt_cache_key` | 调用方的值优先；没有时用账号作用域的 session id。请求头同时带同样的 `Session-Id` |
| 其他未知成员 | 原样保留。保留不等于上游接受；上游拒绝会作为该次尝试的错误返回 |

请求头另加 `X-Codex-Routing-Hint: model=<slug>`。lite 模型再加 `X-OpenAI-Internal-Codex-Responses-Lite: true`。UA、`Originator` 和 `Chatgpt-Account-Id` 仍由 `SetChatHeaders` 设置。

Chat/Messages 翻译路径还会先做消息到 item 的重建（tool 结果变成 `function_call_output`，函数工具从 Chat 嵌套形状展开）。那一步不是原生保真；上游约束是在重建之后才套上的。

## 5. 输出和失败

- 原生流式转发上游事件，不压成 chat delta。namespace 工具名在响应里还原。
- 原生非流式只取 `response.completed` 或 `response.incomplete` 上的完整 `response` 对象，不经 `ChatOutcome`。
- `response.failed` 记为错误。事件已经转给客户端时，不再追加第二条错误帧。
- 原生候选耗尽后不自动改走有损的 Chat 重建。没有原生候选且兼容转换也失败时，请求直接失败。
- 协议不兼容不给账号加冷却。上游的限流、认证和可用性错误仍走原来的分类。
- 响应已经开始写给客户端之后不换上游再拼一段。
- 原文、加密内容和凭证不进默认日志。

fake 测试证明字段按上面的清单改写、其余成员留下来、失败终态不会被当成成功。它不证明某个真实 ChatGPT 账号接受全部保留字段，也不证明加密 reasoning 可以跨账号回放。

## 6. 还没有做

这些仍是后续，不是当前能力：

- Chat 和 Messages 的原生入口。同协议优先原生的矩阵里，这两格继续走兼容翻译。
- 一个包住三种入口的统一 `translate.Request`。现在只有 Responses 有独立的原文类型。
- `previous_response_id` 和 `conversation`。要做之前得先定上游状态属于哪个账号、粘性怎么维持、失效和 failover 怎么办。
- 通用协议注册、跨协议转换图、服务端对话存储，以及跨账号的 reasoning 回放缓存。
- 真实账号上的多轮验收。本地测试和这一步仍然分开记。

第二种原生协议真的出现时，再考虑把可选能力收成更通用的形状。在那之前，复用停留在解析、选号、尝试循环、Codex 上游改写和事件判定上。

**边界：ChatRequest 是兼容路径，不是 Responses 的入口。原生协议是 adapter 显式实现的能力。executor 仍是唯一的选号和执行策略中心。**
