# 多上游账号类型：Trae / TraeWork 对照与扩展计划

**已过期。不要按本文实现。**

canonical: [docs/PROVIDERS_TRAE_SOLO.md](./PROVIDERS_TRAE_SOLO.md)（2026-08-29：消费级 Trae CN Solo / `solo_work_lite`，主参考 `connectedGraph/trae2api-web`）

本文锁定的是已归档的 [linqiu919/trae2api](https://github.com/linqiu919/trae2api) 国际版 IDE 通道（`POST a0ai-api-sg.byteintlapi.com/api/ide/v1/chat`）。后来核对过：

- 消费级 Trae CN 实际走 `llm_utils_chat` + `function=solo_work_lite`，不是这篇的 `/api/ide/v1/chat`
- 官方 `traecli` 是企业旗舰 Coco agent（`/api/ide/v2/llm_raw_chat`），不能当 qodercli 式 worker
- 硬编码 Claude/GPT 映射、`CODING_TOKEN`、随机设备头、`request_wait_in_queue` 都不适用于 Solo

保留本文只作国际版 IDE 协议档案。T0–T5、descriptor、payload、SSE 一律以 `PROVIDERS_TRAE_SOLO.md` 为准。

---

last-updated: 2026-08-28
status: **superseded 2026-08-29**。仅协议调研与草案；未开工。排在 WorkBuddy 验收之后（见 `docs/PLAN.md` Phase O）。WorkBuddy in-process adapter 是前置验证，未通过真实账号验收前不实现 Trae 流量。
trae-reference: [linqiu919/trae2api](https://github.com/linqiu919/trae2api) `main` @ `9486255`（V1.1.2，已停止维护，不支持 Trae v1.3.0+ 新模型）
workbuddy-anchor: [docs/PROVIDERS.md](./PROVIDERS.md) 的 WorkBuddy 章节（第一个 in-process provider，本草案的扩展轴同款）
qoder-anchor: [docs/DESIGN.md](./DESIGN.md) 的控制面 / 执行面边界；[docs/PLAN.md](./PLAN.md) Phase O 的 TraeWork survey 待办

本文回答三件事：

1. Trae（字节 Trae 国际版 / TraeWork）能不能作为本仓库的新 `provider`。
2. 参考 trae2api 的协议后，当前仓库的架构是否够用、哪里必须先改。
3. 若要做，完整怎么做，以及和已经落地的 WorkBuddy adapter 怎么复用。

当前仍以 Qoder 里程碑为准。Phase L（Qoder CN L6）与 WorkBuddy 真实账号验收未完成前，不实现 Trae 流量。硬规则见 `AGENTS.md`，Qoder 运行时见 `docs/DESIGN.md`，当前清单见 `docs/PLAN.md`。

> **重要边界**：trae2api 已声明停止维护、模型表硬编码、且默认走伪造设备头 + 预设 `CODING_TOKEN` 绕过登录。本草案**只借它的协议事实**（端点、头、SSE 形态、模型名映射、错误形态），**不借**它的停止维护债务、硬编码模型表、或绕过正规登录的灰色做法。接入时走正规 OAuth device-flow 登录。

## 结论

**可以参考，不能当 drop-in 账号类型。**

| 问题 | 答案 |
|------|------|
| 值不值得做成新 `provider` | 值得。Trae 是「自有登录态 → OpenAI 兼容 API」的同类上游，和本仓库产品形态接近，且是 PLAN.md Phase O 明文排定的 candidate #2。 |
| 现在能不能直接加进 Accounts | 不能。SQLite、凭证、登录、执行器、模型目录、调度信号仍以 Qoder/WorkBuddy 为中心；Trae 需先作为第二个 in-process adapter 落地。 |
| 现有仓库能不能扩展到 Trae | 能。WorkBuddy 已经把「控制面带 provider 的账号注册表 + in-process adapter」这条扩展轴打通；Trae 复用同一套 `providers.Adapter` 接口即可。 |
| 该不该整仓移植 trae2api | 不该。trae2api 是一个成品网关（含它自己的鉴权、Redis、重试），嵌进来等于多一层无意义代理，且继承它「停止维护 + 硬编码模型」的债务。只借协议。 |
| Trae 要不要 Node / WASM worker | 不要。Trae 是纯 HTTP/SSE + IDE token，没有进程内全局 AuthManager，和 WorkBuddy 一样走 Go in-process adapter。 |
| Trae 和 WorkBuddy / Cursor 的关系 | 同一条扩展轴。WorkBuddy 是第一个 in-process 上游；Trae 是第二个，协议比 Cursor 干净（无独立 agent 协议、SSE 已是结构化事件），Cursor 仍最晚。 |
| 模型能不能跨 provider 池化 | 能，但必须做成显式 Route Pool：同一个 public model ID 下的 provider / native model / account 候选集合，不能把所有账号混成一个无约束池。 |

一句话：本仓库已经有「控制面 + in-process adapter」扩展点（WorkBuddy 验证过）。Trae 适合作为第二个 in-process provider，用来复用扩展点，而不是把另一套网关嵌进来。trae2api 是这份 survey 的现成参考材料。

## Trae 是什么

Trae 是字节跳动出品的类 Cursor 编码辅助工具（国际站 `trae.ai`，国内站 `trae.cn`）。trae2api 把 **Trae 国际版 IDE API**（`a0ai-api-sg.byteintlapi.com`）的登录态，转成本地 OpenAI 兼容接口（`/v1/chat/completions`、`/v1/models`）。注意：trae2api 对接的是 **IDE / cloudide 通道**，不是 Trae 的网页对话；这与本仓库对接 Qoder cloud API 的形态一致。

它和本仓库的相似点：

- 自托管，单 API key（trae2api 用 `AUTH_TOKEN`，本仓库用首次启动生成的 key）
- 多账号轮转、冷却、failover（trae2api 单实例单账号，但理论可多实例；本仓库已在 Go 控制面统一做）
- OpenAI `POST /v1/chat/completions` + SSE
- 工具调用、`reasoning_content`（trae2api 用 `<think>` 包裹 reasoning，本仓库已在 WorkBuddy 路径处理 `reasoning_content`）
- 设备授权登录（trae2api 用 OAuth `ExchangeToken`；本仓库用浏览器 device-flow / PAT / `qoder-native-v1` 导入）

它和本仓库的本质差异：

| 维度 | CLI2API（本仓库） | trae2api |
|------|-------------------|----------|
| 上游 | Qoder cloud + 本地 pinned qodercli WASM；WorkBuddy HTTP | Trae IDE/cloudide HTTP API |
| 运行时 | Go 控制面 + 每账号 Node daemon（Qoder）/ in-process（WorkBuddy） | 纯 Go，无子进程 |
| 凭证 | 加密 `.auth/user` blob + `machine_id`（Qoder）；accessToken/uid（WorkBuddy） | `APP_ID` / `CLIENT_ID` / `REFRESH_TOKEN` / `USER_ID` → `x-ide-token`；或预设 `CODING_TOKEN` |
| 登录 | 浏览器 device-flow / PAT / 凭证导入 | OAuth `ExchangeToken`（trae2api 默认 `CODING_MODE=true` 直接用预设 JWT，绕过登录） |
| 对话入口 | worker 内 `prepareInferRequest` → `agent_chat_generation?Encode=1`（Qoder）；`/v2/chat/completions`（WorkBuddy） | `POST {BASE_URL}/api/ide/v1/chat` |
| 模型目录 | 登录后 in-process catalog（Qoder）；`/console/enterprises/personal/models`（WorkBuddy） | `GET {BASE_URL}/api/ide/v1/model_list?type=chat`（trae2api 也硬编码一份 fallback） |
| 账号存储 | SQLite + 派生 runtime HOME | 环境变量 + 可选 Redis 缓存 refresh_token |
| SSE 形态 | Qoder 自定义；WorkBuddy 已是 OpenAI chunk | Trae 自有事件：`event: request_wait_in_queue` / `event: output` / `event: done` |
| 产品附加 | 控制台、托管更新、Anthropic 协议规划 | 排队重试、claude3.7 截断自动续答（`继续`）、Redis token 缓存 |
| 维护状态 | 活跃 | **已停止维护，不支持 v1.3.0+ 新模型** |

参考价值高的是 **OAuth 换 token、chat 头、SSE 事件结构、模型名映射、队列重试、自动续答、错误分类**。参考价值低的是账号落盘方式、进程模型、硬编码模型表、绕过登录的 `CODING_TOKEN`。

## 上游协议（从 trae2api 抽出，需真实账号复核）

> 以下端点 / 头 / 字段来自 trae2api V1.1.2 源码（`api/handler.go`、`config/token.go`、`config/device.go`）。trae2api 已停止维护，**接入前必须用真实 Trae 账号抓包复核**，尤其是 v1.3.0+ 是否改了端点或事件名。

### 登录 / Token 获取

两种模式，trae2api 默认走模式 B：

- **模式 A — OAuth RefreshToken 交换**（正规，应被本仓库采用）：
  1. `POST {REFRESH_TOKEN_URL}/cloudide/api/v3/trae/oauth/ExchangeToken`
     - Body：`{ "ClientID", "RefreshToken", "ClientSecret":"-", "UserID" }`
     - 先用 `RefreshToken` 换新一轮 `RefreshToken`（`Result.RefreshToken` / `Result.RefreshExpireAt`）
     - 再用新的 `RefreshToken` 换 `Token`（`Result.Token` / `Result.TokenExpireAt`）
  2. `Token` 即后续 chat 头里的 `x-ide-token`
  3. 提前 5 分钟刷新；`refreshExpireAt` 到期视为凭证失效，账号应禁用并要求重登
  4. 可选 Redis 缓存 `TOKEN:<appID>` / `REFRESH_TOKEN:<appID>`，容器重启后优先用 Redis 中的值

- **模式 B — 预设 Coding Token**（trae2api 默认 `CODING_MODE=true`，**本仓库不应采用**）：
  - 直接用环境变量里的 `CODING_TOKEN`（一个硬编码 JWT），跳过 OAuth
  - 这是绕过正规登录的灰色做法，违反本仓库「只用你有权使用的账号」原则，且 JWT 会过期

本仓库应实现**模式 A**，并提供一个浏览器 device-flow 或 PAT 风格的登录入口（参考 WorkBuddy `StartLogin` / `PollLogin`），把 `APP_ID/CLIENT_ID/REFRESH_TOKEN/USER_ID` 作为 `trae-oauth-v1` 凭证 JSON 落盘到 SQLite。

### Chat

- URL：`POST {BASE_URL}/api/ide/v1/chat`，`BASE_URL` 默认 `https://a0ai-api-sg.byteintlapi.com`
- **上游请求体是 Trae 自有结构，不是 OpenAI 格式**。trae2api 把 OpenAI messages 翻译成：
  - `user_input`：最后一条用户消息文本
  - `intent_name`: `"general_qa_intent"`
  - `variables`：JSON 字符串，含 `language`、`locale:"zh-cn"`、`input`、`version_code`、`workspace_path`、`brand:"Trae"`、`system_type:"Windows"` 等
  - `context_resolvers`：固定两项 `project-labels` / `terminal_context`
  - `chat_history`：前面轮次的 role/content/status/ locale，按 `session_id` 归并
  - `session_id` / `conversation_id`：由首轮消息哈希派生并缓存（**多轮靠这个维持**）
  - `current_turn`、`valid_turns`、`model_name`（Trae 内部名）、`last_llm_response_info`、`is_preset:true`
- 请求头（来自 `setRequestHeaders`，按顺序、且 `req.Host` 要覆盖成 `BASE_URL` 的 host）：

  ```
  Content-Type: application/json
  x-app-id:            <APP_ID>
  x-ide-version:       1.2.10
  x-ide-version-code:  20250325
  x-ide-version-type:  stable
  x-device-cpu:        AMD
  x-device-id:         <随机数字>
  x-machine-id:        <64 位十六进制>
  x-device-brand:      92L3 / 91C9 / ...（随机选）
  x-device-type:       windows
  x-ide-token:         <ExchangeToken 换来的 Token>
  accept:              */*
  Connection:          keep-alive
  User-Agent:          （空）
  Host:                <BASE_URL 的 host>
  ```

  ⚠️ **设备头伪造是 trae2api 的合规灰区**。本仓库接入时应复用账号自身真实的 device 指纹（或一次性生成并持久化到凭证，而非每次随机），避免「每 3 次请求换一套设备」这类易被风控的行为。

- **content 预处理**：trae2api 把 OpenAI 的数组 content（多模态）只取第一条 text；其余类型 `fmt.Sprintf("%v")` 兜底。本仓库应改为：文本正常透传，多模态走 Trae 支持的 `multi_media` 字段（若支持），不支持则按 `isModelSupported` + 能力表拒绝而非静默丢内容。

- **模型名映射**（OpenAI 公开名 ↔ Trae 内部名），trae2api `convertModelName`：

  | OpenAI 公开名 | Trae 内部名 |
  |---------------|-------------|
  | `claude-3-5-sonnet-20240620` / `...-20241022` / `claude-3-5-sonnet` | `claude3.5` |
  | `claude-3-7-sonnet-20250219` / `claude-3-7-sonnet` / `claude-3-7` | `aws_sdk_claude37_sonnet` |
  | `gpt-4o-mini` / `gpt-4o-latest` | `gpt-4o` |
  | `gpt-4.1` / `gpt-4-1` / `gpt-4.1-2025-04-14` | `gpt-4.1-2025-04-14` |
  | `deepseek-chat` / `deepseek-coder` / `deepseek-v3` | `deepseek-V3` |
  | `deepseek-reasoner` / `deepseek-r1` | `deepseek-R1` |
  | `deepseek-chat-0324` / `deepseek-V3-0324` | `deepseek-V3-0324` |
  | `gemini-2.5-pro-preview-03-25` / `gemini-2.5-pro` | `gemini-2.5-pro-preview-03-25` |
  | `gemini-2.5-flash` | `gemini_2.5_flash` |

  ⚠️ 这是**硬编码且已过期**的（trae2api 不支持 v1.3.0+）。本仓库应从 `model_list` 动态拉取并做公开名归一化（参考 WorkBuddy `Models` 的过滤 + 归一逻辑），不要照搬静态表。

### 模型

- 列表接口：`GET {BASE_URL}/api/ide/v1/model_list?type=chat`
- 响应：`{ "model_configs": [ { "name", "display_name", "is_default", "multimodal", "custom_config" } ] }`
- trae2api 把 `aws_sdk_claude37_sonnet → claude-3-7-sonnet`、`claude3.5 → claude-3-5-sonnet` 做反向映射后输出 OpenAI `ModelResponse`
- 本仓库应：调 `model_list` → 走 `convertModelName` 反向（内部名 → 公开名）→ 用 `providers.ModelInfo{NativeModel, PublicModel, DisplayName, Capabilities}` 接入账号级模型目录，失败回退静态表（但要标注「可能过期」）

### SSE 事件结构

Trae 不是标准 OpenAI chunk，而是自有事件流（`bufio` 按行读 `event: ` / `data: `）：

- `event: request_wait_in_queue` → `{ "position", "message", "queue_id }`：排队中。trae2api 非流式忽略，流式每 5s 向客户端推一条「排队中，当前位置：N」的 OpenAI chunk。**本仓库应映射成可观测的 queue 信号**（参考 WorkBuddy 的排队重试，但不要用 `content` 冒充模型输出，建议走 `X-*` 响应头或 status 事件，待定）。
- `event: output` → `{ "response", "reasoning_content", "finish_reason" }`：核心输出。
  - `reasoning_content` 非空 → 用 `<think>\n\n` 包裹首段、`\n\n` 续段，映射为 OpenAI `delta.reasoning_content`（本仓库 `translate` 层已支持 thinking，见 WorkBuddy `Aggregate`）
  - `response` 非空且 reasoning 已结束时 → 用 `</think>\n\n` 收尾再接 `response`
  - `finish_reason` 记录到 done
- `event: done` → `{ "finish_reason" }`：结束。trae2api 在 `finish_reason == "length"` 且模型是 `aws_sdk_claude37_sonnet` 且 `AUTO_CONTINUE_ENABLED=true` 时，自动把已输出内容 + `继续` 拼回 messages 再发一次请求（递归）。**本仓库的自动续答应走 executor 层的截断续答策略，而不是在 adapter 里递归伪造 `gin.Context`**（trae2api 那招在 CLI2API 控制面下不安全）。

**SSE 解析不能复用 WorkBuddy 的 `sse.Aggregate`**（事件名不同：`output`/`done` vs OpenAI chunk）。但 `mergeToolCalls` / 按 index 合并 tool_calls 的辅助函数可复用，需新建 `internal/providers/trae/sse.go` 写 Trae 专属解析。

### 积分 / 配额信号

trae2api **没有**配额/积分查询接口——所有调用受 Trae 自身限额与排队限制，超限额表现为 `request_wait_in_queue` 长时间占位或 429。因此 Trae provider 的 `Quota` 应：
- 先返回 `nil`（display-only，缺失不报警，符合 `AccountProber` 契约）
- 把排队超时 / 429 通过 `Classify` 映射到 `cooldown` + `failover`，让调度层做账号冷却与切换
- 若未来 Trae 暴露 billing 接口，再补 `Quota`（参考 WorkBuddy `UserResource` 的聚合写法，**不要**照搬 workbuddy2api 的「按剩余积分选号」调度——本仓库保持 round-robin + pin + failover）

### 错误分类对照

来自 trae2api `CreateChatCompletion` 的状态码映射 + Trae 业务错误：

| 上游状态 / 信号 | trae2api 处理 | 本仓库 `ClassifiedError` |
|----------------|---------------|--------------------------|
| 401 Unauthorized | 报 `unauthorized` | `kind=auth`, `Failover=false`, 账号禁用要求重登 |
| 403 Forbidden | 报 `permission_denied` | `kind=auth`, `Failover=false` |
| 404 Not Found | 报 `not_found` | `kind=not_found`, `Failover=false` |
| 429 Too Many Requests | 报 `rate_limit_exceeded` | `kind=rate_limit`, `Failover=true`, `Cooldown=短` |
| 400 / `code=11101` 类业务错 | `invalid_request` | `kind=invalid_request`, `Failover=false`（参数问题，换账号没用） |
| 5xx / 网络错 | `service_unavailable` | `kind=upstream_error`, `Failover=true` |
| `request_wait_in_queue` 超过重试上限 | 流式推「排队中」 | `kind=queue_timeout`, `Failover=true`, `Cooldown=中` |
| `refreshExpireAt` 到期 | 401 token_expired | `kind=auth`, 触发 `Probe` 重登 |

> 重试策略：trae2api 对排队最多重试 3 次、每次 sleep 3s。**本仓库不应在 adapter 里 sleep 重试**——排队应交给 executor 的 failover 循环 + 控制面冷却，保持 adapter 无状态、可中断（`context.Context` 取消即停）。

## 当前仓库的真实扩展性

### 已经可复用（WorkBuddy 已验证）

- `providers.Adapter` 接口（Credential / Login / Chat / Models / Classifier / ImportExport / Prober）—— Trae 只需实现同样一组方法，注册到 `providers/registry.go`
- `providers.ProviderDescriptor` + `RegionDescriptor` —— 注册 `trae` 描述符，`RuntimeKind=in_process`，region `global`（Trae 国际版；若接 `trae.cn` 再加 `cn` region，类比 Qoder CN 不要新建 provider family）
- `translate.ChatRequest` / `ChatOutcome` —— 内部会话契约，Trae adapter 在边缘做 OpenAI↔Trae 转换
- `internal/accounts` 的 SQLite 账号注册表、`materializeHome` / `SyncCredential` 的凭证落盘与回写（Trae 无 HOME 需求，但凭证 JSON 写入 SQLite 的路径可复用）
- `internal/executor/chat.go` 的 round-robin + pin + 冷却 + failover 循环（Trae 作为 in-process 候选账号自然接入，无需改调度核心）
- `internal/translate` 的 thinking / tool_calls 映射（WorkBuddy 已验证）
- 控制台 `AddAccountModal` 的 provider 选择器（前端已有类型选择器，加 `trae` 选项 + i18n key 即可）

### 现在还不能扩展的点（需先确认 / 小改）

- **Route Pool 跨 provider 同名模型**：Phase M/N 规划的 `CROSS_PROVIDER_MODEL_POOL` 尚未落地。Trae 的 `claude-3-7-sonnet` 与 Qoder/WorkBuddy 的同名模型要池化，必须等显式 Route Pool 实现，不能默认混池（结论表已强调）。
- **自动续答位置**：trae2api 在 adapter 内递归续答；本仓库应放到 executor 层（截断 `finish_reason=length` 时由控制面决定续答），Trae adapter 只暴露「是否支持续答」能力，不自己递归。
- **排队信号透传**：Trae 的 `request_wait_in_queue` 需要定义本仓库内部的 queue 信号（建议走响应头或 status SSE 事件），不能像 trae2api 那样用 `content` 冒充输出。
- **设备指纹合规**：trae2api 随机伪造设备头是灰区；本仓库需定义「每账号持久化一份真实 device 指纹」的存储位置（可放进 `trae-oauth-v1` 凭证 JSON），不要照搬随机刷新逻辑。

### 架构判断

和 WorkBuddy 完全一致：本仓库已经有「控制面 + in-process adapter」雏形，且 WorkBuddy 已验证扩展点可用。Trae 是第二个 in-process provider，**不需要改调度核心、不需要 WASM worker、不需要新进程模型**。真正要写的是 `internal/providers/trae/` 这一个包 + registry 注册 + 前端选项，加上一份 `model_list` 动态归一（不做硬编码表）。trae2api 的价值到此为止——它是协议参考，不是要嵌入的组件。

## 目标架构

与 WorkBuddy 同构。Trae 作为 `RuntimeKind=in_process` 的第二个 provider：

```
OpenAI 客户端
   │  POST /v1/chat/completions   (auth: 本仓库 API key)
   ▼
internal/api/chat.go  ──► internal/executor/chat.go  (round-robin + pin + 冷却 + failover)
                               │  pick 一个就绪账号（含 trae 账号）
                               ▼
                    providers.Registry.Adapter("trae")
                               │  调用 Trae adapter 的 ChatStream / ChatNonStream
                               ▼
                  internal/providers/trae/   (纯 Go HTTP/SSE，无子进程)
                               │  ExchangeToken 换 x-ide-token + 设备头 + POST /api/ide/v1/chat
                               ▼
                          Trae cloudide API (a0ai-api-sg.byteintlapi.com)
```

关键边界（沿用 `AGENTS.md` 的 auth / endpoint / executor / translate / api 分层）：
- `internal/providers/trae/` 只负责 Trae 协议：token 交换、header 拼装、请求体构造、SSE 解析、错误分类、模型名归一。
- `internal/executor/chat.go` 只负责账号选择、冷却、failover、续答决策——不感知 Trae 细节。
- `internal/translate` 只负责 OpenAI ↔ 内部会话契约——Trae 的 `reasoning_content` / `tool_calls` 在 adapter 边缘转成 `translate.ChatOutcome`。

### Provider descriptor 与能力接口

新增 `trae` 描述符（参考 `providers/registry.go` 的 `ProviderDescriptor`）：

```go
ProviderDescriptor{
    ID:            "trae",
    Label:         "Trae",
    Runtime:       RuntimeInProcess,
    AuthTypes:     []AuthType{AuthOAuth},            // 对应 trae-oauth-v1；不提供 CODING_TOKEN 绕过
    CredentialFormats: []string{"trae-oauth-v1"},    // APP_ID/CLIENT_ID/REFRESH_TOKEN/USER_ID (+ 持久化 device 指纹)
    Capabilities: ProviderCapabilities{
        Chat:true, Stream:true, Tools:true, Images:false, // 多模态待 model_list 复核
        Reasoning:true, ModelCatalog:true, Usage:false,   // Trae 无 usage 返回
        Login:true, BrowserLogin:true, PATLogin:false, ImportExport:true,
    },
    Regions: []RegionDescriptor{
        {ID:"global", Label:"Trae 国际版", ChatBase:"https://a0ai-api-sg.byteintlapi.com",
         AuthBase:"https://api-sg-central.trae.ai", DefaultDomain:"byteintlapi.com"},
        // 若接 trae.cn：{ID:"cn", Label:"Trae 国内版", ChatBase:"...", ...} —— 不要新建 provider family
    },
    DefaultRegion: "global",
}
```

`Adapter` 实现（对照 `providers.Adapter`）：

| 接口 | Trae 实现要点 |
|------|---------------|
| `Credential` | `Validate` 校验 `trae-oauth-v1` JSON 字段完整；持久化 device 指纹 |
| `Login` | `StartLogin` 触发 OAuth device-flow（或引导用户贴 REFRESH_TOKEN）；`PollLogin` 轮询直到 `ExchangeToken` 成功 |
| `Chat` | `ChatStream` 返回 `*http.Response`（Trae SSE）；`ChatNonStream` 聚合 SSE 成 `ChatOutcome` |
| `Models` | 调 `model_list`，内部名→公开名归一，失败回退静态表（标注过期） |
| `Classifier` | 上表错误分类映射 |
| `ImportExport` | `ValidateImport` / `Export` 处理 `trae-oauth-v1` JSON |
| `Prober` | `Probe` 用凭证做一次轻量 `model_list` 或 token 校验判就绪；`Quota` 返回 `nil`（暂无配额接口） |

### Runtime kind

`RuntimeInProcess`——和 WorkBuddy 一致，**不 spawn 子进程、不依赖 qodercli/WASM**。

### 账号标识

`provider=trae` + `region=global`（或 `cn`）。复用 `internal/accounts` 的账号表，`AccountView` 通过 `/api/accounts` 与 `/api/overview` 携带 Trae 专属状态（就绪、UID、冷却、最后错误）。Trae 无 `machine_id`/HOME 需求，但 device 指纹随凭证持久化。

## 数据模型

复用 `internal/accounts` 现有 schema（H1 已落地）。Trae 只需在 `credential_payload` 列存 `trae-oauth-v1` JSON：

```json
{
  "format": "trae-oauth-v1",
  "app_id": "...",
  "client_id": "...",
  "refresh_token": "...",
  "user_id": "...",
  "device": {
    "device_cpu": "AMD",
    "device_id": "<持久化，不随机刷新>",
    "machine_id": "<64  hex，持久化>",
    "device_brand": "92L3",
    "device_type": "windows"
  },
  "token": "<ExchangeToken 换来的 x-ide-token，运行时刷新>",
  "token_expire_at": 1710000000000,
  "refresh_expire_at": 1710000000000,
  "region": "global"
}
```

- 不引入新的账号池文件、不引入 Redis 专属槽（trae2api 的 Redis 缓存是它自己的设计，本仓库用 SQLite + 内存即可；若要多实例共享 token，走本仓库既有的 `/data` 卷，而非照搬 Redis）。
- 不照搬 trae2api「每 3 次请求换设备」逻辑——device 指纹随账号固定。

## 登录与控制台

- **登录方式**：浏览器 device-flow 或引导用户贴 `REFRESH_TOKEN`（对应 trae2api 的 OAuth `ExchangeToken`）。**不提供 `CODING_TOKEN` 绕过入口**（合规）。
- **控制台**：`AddAccountModal` 增加 `trae` / `trae-cn` 选项（i18n key `account_provider_trae`），PAT 帮助文案指向 Trae 的 token 获取文档。账号卡片复用现有 UID / 就绪 / 冷却 / 最后错误 字段。
- **配额显示**：Trae 无配额接口，`Quota` 返回 `nil`，账号卡片不渲染进度条（与 `AccountProber` 契约一致，缺失不报警）。

## 执行路径

对照 WorkBuddy 的 `internal/providers/workbuddy/` 文件结构，Trae 新建：

```
internal/providers/trae/
  client.go      // Client 结构、do()、ExchangeToken、StartLogin/PollLogin、Models、ChatNon/ChatStream、Classify、Probe/Quota、Adapter()
  credential.go  // DecodeCredential / Encode / ValidateCredential / Ready / ChatBase
  headers.go     // setTraeHeaders（x-app-id / x-ide-token / x-device-* / Host 覆盖）
  payload.go     // OpenAI messages → Trae TraeRequest（user_input / variables / chat_history / session_id）
  sse.go         // Trae 事件解析：request_wait_in_queue / output / done → ChatOutcome / 流式 chunk
  client_test.go // httptest 契约测试（参考 workbuddy client_test.go 的 11 个测试模式）
```

- `sse.go` 的 `Aggregate`（非流式聚合）+ 流式 `ChatStream` 返回 `*http.Response` 给 `internal/api/chat.go` 直接 relay，结构与 WorkBuddy 同构，但事件名解析不同。
- `mergeToolCalls` 辅助函数可抽进 `internal/translate` 共用，避免 Trae / WorkBuddy 各写一份。

## 同名模型跨 Provider Route Pool

沿用 `docs/PROVIDERS.md` 的 Route Pool 规则：Trae 的 `claude-3-7-sonnet` 与 Qoder / WorkBuddy 同名模型要池化，**必须等 `CROSS_PROVIDER_MODEL_POOL` 显式开启**（Phase M/N 规划），不能默认混池。

- public model ID 归一后，Route Pool 记录该 ID 下的候选：
  `{provider: trae, native: aws_sdk_claude37_sonnet, account: acc_trae_1}`、`{provider: qoder, native: claude-3-7-sonnet, account: acc_qoder_2}` …
- 调度仍优先同 provider/region 候选，跨 provider 仅在同池且开启开关时逃逸。
- Trae 的 `deepseek-V3` / `gemini_2.5_flash` 等若与其他 provider 同名，同样走此规则。

## 调度与后台任务

- **排队重试**：trae2api 在 adapter 内 sleep 重试 3 次——本仓库**不搬**。排队信号经 `Classify` 映射成 `queue_timeout` + `Failover=true` + `Cooldown`，交给 executor 的 failover 循环与冷却，adapter 保持无状态、可被 `context.Context` 取消。
- **自动续答**：`finish_reason == "length"` 且模型支持续答时，由 executor 层决定（把已输出 + 续答提示拼回 messages 再发），**不在 Trae adapter 内递归伪造请求**（trae2api 的 `CreateChatCompletion(newContext)` 递归在 CLI2API 控制面下不安全）。
- **token 保活**：`Probe` 周期内在 `token_expire_at` 前 5 分钟刷新（复用 trae2api 的提前刷新逻辑），刷新失败只记日志 + 标账号未就绪，不冷却、不影响其他账号。

## 详细实施计划

> 前置：Phase L（Qoder CN L6）验收完成 + WorkBuddy 真实账号验收通过（PLAN.md Phase O 前置条件）。以下阶段编号接在 WorkBuddy J0–J4 之后，作为 **T0–T5**（Trae）。

### T0 — 控制面承认 `trae` provider
- [ ] `providers/registry.go` 注册 `trae` 描述符（region `global`，预留 `cn`）
- [ ] `internal/accounts/store.go` 的 provider/region 校验接受 `trae` + `global`（参考 `TestStoreRejectsUnknownProviderAndRegion` 改法，类比 L0）
- [ ] 前端 `AddAccountModal` 增加 `trae` 选项 + i18n key

### T1 — 凭证与登录
- [ ] `credential.go`：`trae-oauth-v1` 校验、Encode/Decode、持久化 device 指纹
- [ ] `client.go`：`ExchangeToken` 换 `x-ide-token`（模式 A），提前 5 分钟刷新
- [ ] `client.go`：`StartLogin` / `PollLogin`（device-flow 或贴 REFRESH_TOKEN），成功落盘凭证
- [ ] `ImportExport`：`ValidateImport` / `Export` 处理 `trae-oauth-v1`

### T2 — Chat adapter（最小闭环）
- [ ] `payload.go`：OpenAI messages → TraeRequest（user_input / variables / chat_history / session_id 缓存）
- [ ] `headers.go`：setTraeHeaders（含 `Host` 覆盖）
- [ ] `sse.go`：Trae 事件解析（output / done / request_wait_in_queue）
- [ ] `client.go`：`ChatStream` / `ChatNonStream` 走 `providers.ProviderChat`

### T3 — 模型与错误分类
- [ ] `client.go`：`Models` 调 `model_list`，内部名→公开名归一，失败回退静态表（标注过期）
- [ ] `client.go`：`Classify` 按上表映射（auth / rate_limit / queue_timeout / upstream_error …）
- [ ] `client.go`：`Probe` 轻量判就绪；`Quota` 返回 `nil`

### T4 — 池质量与续答（可选，建议紧跟 T2）
- [ ] executor 层截断续答决策（不在 adapter 递归）
- [ ] 排队信号透传定义（响应头或 status 事件，不用 content 冒充）
- [ ] `mergeToolCalls` 抽到 `internal/translate` 共用

### T5 — 同名模型 Route Pool 与协议
- [ ] 等 `CROSS_PROVIDER_MODEL_POOL` 开启后，Trae 同名模型进 Route Pool
- [ ] httptest 契约测试（参考 workbuddy `client_test.go` 的 11 个模式：登录轮询、非流式聚合 tools+reasoning、模型过滤、错误映射、header region 特定、Probe 就绪、Quota 缺失…）
- [ ] 真实 Trae 账号验收：`只回复OK`、多账号 failover、截断续答、工具调用

## Trae 参考实现与代码使用边界

### 参考对象
- `linqiu919/trae2api` `main` @ `9486255`（V1.1.2）：协议事实的唯一来源。

### 可参考并重写的代码
- `api/handler.go` 的 `setRequestHeaders`：Trae chat 头顺序与字段（x-app-id / x-ide-token / x-device-*）
- `api/handler.go` 的 `convertModelName`：OpenAI 公开名 ↔ Trae 内部名映射（**改成动态 + 回退，不硬编码**）
- `config/token.go` 的 `ExchangeToken` 两阶段换 token 流程 + 提前刷新
- `api/handler.go` 的 SSE 事件解析（output / done / request_wait_in_queue）结构
- `config/device.go` 的 device 字段集合（**改为持久化，不随机刷新**）

### 不使用或不照搬的代码
- **整仓移植**：trae2api 自带 Gin 服务、Redis、鉴权中间件——本仓库已有控制面，不嵌
- **硬编码模型表**：`isModelSupported` 的静态列表已过期（不支持 v1.3.0+），改用 `model_list` 动态
- **`CODING_MODE` / `CODING_TOKEN` 绕过登录**：合规灰区，本仓库不采用
- **每 3 次请求随机换设备**：易被风控，改为每账号持久化一份 device 指纹
- **adapter 内 `sleep` 重试排队 + 递归续答**：交给本仓库 executor 的 failover / 冷却循环
- **用 `content` 冒充「排队中」输出**：改为内部 queue 信号透传

### 引用原则
只借协议事实，不借维护状态与架构。trae2api 已停止维护，**所有端点 / 事件名 / 头字段在开工前必须用真实 Trae 账号抓包复核**，尤其是 v1.3.0+ 的兼容性变化。

## 风险

1. **上游变更风险**：trae2api 已停更且不支持 v1.3.0+，Trae 官方可能改端点 / 事件名 / 鉴权。缓解：开工前抓包复核；`model_list` 动态拉取；token 刷新失败即标账号未就绪。
2. **合规风险**：trae2api 的伪造设备头 + 预设 JWT 绕过登录是灰区。缓解：走正规 OAuth、device 指纹持久化且真实、README 强调「只用你有权使用的账号」。
3. **排队体验**：Trae 限额靠 `request_wait_in_queue` 表现，无结构化配额。缓解：queue 信号映射成冷却 + failover，控制台显示排队状态而非塞进模型输出。
4. **里程碑约束**：`AGENTS.md` / `PLAN.md` 规定当前 Qoder 里程碑（含 L6、Phase I）未完成前不启动非 Qoder 上游。缓解：T0–T5 排在 WorkBuddy 真实验收之后，或先显式 defer 当前里程碑。
5. **多模态未知**：trae2api 只取数组 content 第一条 text，Trae 多模态能力未核实。缓解：`Images` 能力默认 `false`，待 `model_list` + 抓包确认后再开。

## 对本仓库可用性的判断

本仓库**已经具备**接入 Trae 的扩展点：WorkBuddy 验证了「控制面带 provider 的账号注册表 + in-process adapter」这条轴完全可行。Trae 作为第二个 in-process provider，不需要改调度核心、不需要 WASM worker、不需要新进程模型。真正要写的是 `internal/providers/trae/` 一个包 + registry 注册 + 前端选项 + 一份动态模型归一。trae2api 是这份 survey 的现成参考，不是要嵌入的组件。

## 建议的产品决策（实现前锁定）

1. **只借协议，不嵌网关**：不移植 trae2api 整仓，新建 `internal/providers/trae/` 复用 `providers.Adapter`。
2. **走正规 OAuth，禁 `CODING_TOKEN`**：device 指纹持久化且真实，不随机刷新。
3. **动态模型，不硬编码**：从 `model_list` 拉取 + 公开名归一，失败回退静态表但标注「可能过期」。
4. **排队 / 续答交给控制面**：adapter 无状态、可取消；排队映射成冷却 + failover，续答由 executor 决策。
5. **里程碑前置**：T0–T5 排在 Phase L6 + WorkBuddy 真实验收之后，或显式 defer。
6. **空间隔离**：Trae 国际版 `region=global`，国内版 `region=cn`，**不要新建 `trae` 之外的 provider family**（类比 Qoder CN 不建 `qodercn`）。
