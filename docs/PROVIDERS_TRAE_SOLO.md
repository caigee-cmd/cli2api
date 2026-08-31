# 多上游账号类型：Trae CN Solo 对照与扩展计划

last-updated: 2026-08-29
status: 协议调研完成；未开工。排在 WorkBuddy 真实账号验收之后（见 `docs/PLAN.md` Phase O）。未通过前置验收前不实现 Trae 流量。
supersedes: [docs/PROVIDERS_TRAE.md](./PROVIDERS_TRAE.md)（2026-08-28 国际版 IDE `/api/ide/v1/chat` 草案，已过期，不要按它实现）
solo-reference: [connectedGraph/trae2api-web](https://github.com/connectedGraph/trae2api-web) `main` @ 2026-08-25（fork 自 [Sliverkiss/traework2api](https://github.com/Sliverkiss/traework2api)）
workbuddy-anchor: [docs/PROVIDERS.md](./PROVIDERS.md) 的 WorkBuddy 章节（第一个 in-process provider，本草案复用同一条扩展轴）
qoder-anchor: [docs/DESIGN.md](./DESIGN.md) 的控制面 / 执行面边界

本文回答四件事：

1. 消费级 Trae CN Solo 能不能作为本仓库的新 `provider`。
2. 官方 `traecli` / 企业 TraeCode CLI、已归档的国际版 IDE 代理，分别为什么不是当前实现路径。
3. 参考 `trae2api-web` 之后，当前仓库哪些代码能直接复用、哪些必须新写。
4. 若要做，T0–T5 怎么落地。

硬规则见 `AGENTS.md`。当前仍以 Qoder 里程碑为准；本文不是开工许可。

> **通道锁定**：当前只做 **Trae 国内消费级 Solo 免费 chat**（`function=solo_work_lite`，花 `ide_credits`）。不是国际版 IDE chat，不是企业 TraeCode CLI / Coco，也不是真正的 Trae Work agent（`work_credits` + 本地加密任务）。

## 结论

**可以参考 `trae2api-web` 的协议，不能当 drop-in 网关，也不能走 `traecli` worker。**

| 问题 | 答案 |
|------|------|
| 值不值得做成新 `provider` | 值得。它是「自有登录态 → OpenAI 兼容 API」的同类上游，协议是纯 HTTP/SSE，和 WorkBuddy 同构。 |
| 现在能不能直接加进 Accounts | 不能。descriptor / adapter / 登录回调 / 前端选项都还没有 `trae`。 |
| 该不该整仓移植 trae2api-web | 不该。只借协议（登录、头、payload、SSE、错误码、模型目录）。不借它的文件账号池、按积分选号、自带 `/admin`、自动签到调度。 |
| 官方 `traecli` 能不能当第二个 qodercli | 不能。它是企业旗舰版的 Go 本地完整 agent（Coco），没有可 hook 的 JS/WASM；`AGENTS.md` 禁止每请求 spawn 完整 CLI。 |
| 旧 `linqiu919/trae2api` 还能不能当主参考 | 不能。已归档，打的是国际版 IDE `POST /api/ide/v1/chat`，和 Solo 不是一条通道。 |
| Trae 要不要 Node / WASM worker | 不要。Go in-process adapter。 |
| 模型能不能跨 provider 池化 | 能，但必须等显式 Route Pool；默认不混池。 |

一句话：本仓库已经有「控制面 + in-process adapter」扩展点（WorkBuddy 验证过）。Trae CN Solo 是第二个 in-process provider。协议跟 `trae2api-web`，架构跟 WorkBuddy，进程模型不要碰 `traecli`。

## 三条通道不要混

| 通道 | 产品 | 主入口 | 参考 | 本仓库 |
|------|------|--------|------|--------|
| **Trae CN Solo** | 消费级 IDE 免费 chat | `POST trae-api-cn.mchost.guru/api/agent/v3/llm_utils_chat`，`function=solo_work_lite` | `trae2api-web` | **当前要做** |
| 国际版 Trae IDE | 旧 IDE chat | `POST a0ai-api-sg.byteintlapi.com/api/ide/v1/chat` | 已归档 `linqiu919/trae2api` | 不做；见过期文档 |
| 企业 TraeCode CLI | 旗舰套餐本地 agent | `POST api.enterprise.trae.cn/api/ide/v2/llm_raw_chat` | 官方 `traecli` v0.120.52（Coco） | 不做 worker；无企业号也抓不到 |

`trae2api-web` 的 `docs/RESEARCH.md` 写清了：社区代理走的是 `ide_credits` 的 SOLO 漏斗。真正的 Trae Work agent 依赖本地 `encrypted_prompt_set`，外部构造不了 `create_agent_task`。外面直接打 `llm_raw_chat` 容易 `4011` 硬限流。

官方 CLI 已在 2026-08-29 解包核对：`trae-cli` `v0.120.52` 是 71MB Mach-O，模块路径 `code.byted.org/nextcode/coco/tenant/trae/cli`，配置目录 `~/.trae`，登录 `console.enterprise.trae.cn`，token 前缀 `trae-lt-`。没有 `llm_utils_chat` / `solo_work_lite`。不能 pin、不能 needle hook。

## Trae CN Solo 是什么

把 Trae 国内站（`www.trae.cn`）IDE 登录态，转到本仓库已有的 OpenAI `POST /v1/chat/completions`。上游不是网页对话，是 Solo 的 `llm_utils_chat`。

和本仓库的相似点：

- 自托管，单 API key
- 多账号 round-robin + 冷却 + failover（调度已在 Go 控制面）
- OpenAI chat + SSE
- 工具调用、`reasoning_content`
- 浏览器登录，而不是让用户手工贴长效 key

和 `trae2api-web` 的本质差异：

| 维度 | CLI2API（本仓库） | trae2api-web |
|------|-------------------|--------------|
| 上游（当前） | Qoder WASM + WorkBuddy HTTP | Trae CN Solo HTTP |
| 运行时 | Go 控制面 + 每 Qoder 账号一个 Node daemon；WorkBuddy in-process | 纯 Go 成品网关 |
| 凭证 | SQLite `account_credential_payloads` | `auths/trae-{uid}.json` |
| 登录 | WorkBuddy：上游发 state + poll。Trae 需要本机 callback | 面板一键登录 + `127.0.0.1:18080/authorize` |
| 调度 | round-robin + pin + failover | 按剩余积分最高者优先 |
| 模型 | catalog 失败就报错，不猜 | `get_detail_param` 动态拉；默认 `glm-5.2` |
| 产品附加 | 已有控制台 | 自带 `/admin`、每日签到、积分看板 |

参考价值高的是 **OAuth 换 token、SOLO 头、payload 改写、SSE 事件、`get_detail_param`、错误码、签到/权益包信号**。参考价值低的是账号落盘、按积分选号、自带 UI。

## 上游协议（从 trae2api-web 抽出，开工前用真实账号复核）

钉死的客户端版本与 WorkBuddy 钉 CLI UA、Qoder 钉 qodercli 同一类约束。当前参考实现：

| 常量 | 值 |
|------|----|
| AgentHost | `https://trae-api-cn.mchost.guru` |
| UgHost | `https://api.trae.cn` |
| OAuthHost | `https://api.trae.com.cn` |
| ConsoleHost | `https://www.trae.cn` |
| ClientID | `en1oxy7wnw8j9n` |
| AppID | `6eefa01c-1036-4c7e-9ca5-d891f63bfcd8` |
| IdeVersion | `0.1.52` |
| IdeVersionCode | `20260811` |
| DeviceBrand | `83DG` |
| Function | `solo_work_lite` |

`0.1.43` 会把 `glm-5.3` 打成 `4001`；`0.1.52` 可用。版本头过期时要像 qodercli pin 一样显式升级，不要静默乱猜。

### 登录 / Token

1. 为账号生成并**持久化** `machine_id` / `device_id`（32 hex）。不要每轮登录随机换，也不要每 N 次请求换设备。
2. 打开 `https://www.trae.cn/authorization?...`（带 machine/device 与 callback）。Trae 强制跳到 `127.0.0.1`。
3. 本机只监听 `127.0.0.1` 的 callback（参考默认 `18080/authorize`）。从 query 解析 refresh / user 载荷。
4. `POST {OAuthHost}/cloudide/api/v3/trae/oauth/ExchangeToken`  
   Body：`{ "ClientID", "RefreshToken", "ClientSecret":"-", "UserID":"" }`  
   换 `Token` / `TokenExpireAt` / 新 `RefreshToken` / `RefreshExpireAt`。
5. `POST {OAuthHost}/cloudide/api/v3/trae/GetUserInfo`  
   Body：`{ "ReqSource":"IDE", "IDEVersion" }`，头带 `X-Cloudide-Token`。拿 `UserID` / `ScreenName` / `EnterpriseID`。
6. 提前约 24 小时刷新；`refreshExpireAt` 到期视为凭证失效，账号禁用并要求重登。

也接受粘贴 `trae2api-web` 的 `auths/trae-{uid}.json`（nested `{auth, account}` 或扁平 JSON）走 Import。

本仓库 **不提供** 旧 trae2api 的 `CODING_TOKEN` 绕过。

控制面现状：`LoginSessionProvider` 只有 `StartLogin` / `PollLogin`（见 `internal/api/accounts.go` 的 `login/device`、`login/status`）。Trae **不必改这个接口**。`StartLogin` 在 adapter 进程内起一次性 `127.0.0.1` listener，返回授权 URL；`PollLogin` 读内存 pending。pending 重启丢失，符合「登录态瞬时」。callback 禁止绑 `0.0.0.0`。

浏览器和本仓库不在同一台机器时：文档写明粘贴完整 callback URL 导入，不要做公网 callback。

### Chat

- URL：`POST {AgentHost}/api/agent/v3/llm_utils_chat`
- 上游强制 `stream: true`；对本仓库客户端的非流式请求，在 adapter 内聚合 SSE
- 请求体由 OpenAI JSON 单次改写（`trae2api-web` `PrepareBody`）：

  1. `function` 固定 `"solo_work_lite"`（其它 function 名实测失败）
  2. `stream` 强制 `true`
  3. `model` → 同时写入 `config_name` 与 `model`；空则默认 `glm-5.2`（仅当调用方没带模型；catalog 失败仍要报错，不要用默认值冒充目录）
  4. string `content` → `[{"type":"text","text":...}]`；已是数组则透传
  5. assistant `tool_calls[].function` → `function_call`；无 name 的剔除
  6. `tool_choice`：`"none"` 删 tools；对象 `auto`/`required` 变字符串；`function` 抽 name
  7. `tools[].function.parameters` 若是 object，序列化成 **JSON 字符串**（上游 Go struct 是 string）

- Chat / 模型头（`SOLOHeaders`）：

  ```
  Content-Type: application/json
  Accept: text/event-stream  （流式）或 application/json
  User-Agent: Trae/0.1.52
  Authorization: Cloud-IDE-JWT <accessToken>
  X-Cloudide-Token: <accessToken>
  X-Ide-Token: <accessToken>
  X-Uid: <uid>
  X-App-Id: <APP_ID>
  X-App-Version / X-Ide-Version: 0.1.52
  X-Ide-Version-Code / X-App-Version-Code: 20260811
  X-Ide-Version-Type: stable
  X-Device-Type / X-OS-Version / X-Device-Brand
  X-Machine-Id / X-Device-Id   （账号持久化，缺则不发）
  Request-Traffic-Type: ...
  ```

  签到/权益包走更瘦的 `UgHeaders`（`Authorization: Cloud-IDE-JWT` + `X-User-Region: CN`）。  
  ExchangeToken 只发 JSON + UA，不带 JWT。

### 模型

- `POST {AgentHost}/api/ide/v1/get_detail_param`
- Body 形态：`function`、`config_names: null`、`need_prompt: false`、`poly_prompt: true`
- 用 `config_info_list`：`config_name` → native/public id，`display_config.display_name` → 展示名。空列表当错误。
- live 目录里 `reasoning_effort_config` 和 `context_window_tokens.max` 经常是空的。Max 还要看 `model_extra_config.v2_max_mode_enabled` / `display_config.max_mode` / `is_dollar_max`；推理还要看 `reasoning_effort_options`、`display_contact_config.reasoning.enable`，以及官方客户端实际使用的 `light` / `high` / `extra_high`（发出去映射成 `low` / `high` / `xhigh`）。
- 本仓库原则与 WorkBuddy 相同：**catalog 失败就报错，不回退过期静态表**。`glm-5.2` / `glm-5.3` 只是实测样例，不是白名单。

### SSE

不是 OpenAI chunk，也不能复用 `workbuddy/sse.go` 的 `Aggregate`。事件：

| 事件 | 字段 | 映射 |
|------|------|------|
| `metadata` | `model`, `session_id`, `prompt_completion_id` | 记录，不发 chunk |
| `timing_cost` | | 忽略 |
| `output` | `response`, `reasoning_content`, `tool_calls` | delta content / reasoning / tool_calls（`function_call` → `function`，丢掉 `namespace` / `partial_arguments`） |
| `extra_info` | 完整 thought | 可记录，不重复当输出 |
| `token_usage` | `prompt_tokens`, `completion_tokens`, `total_tokens`, `reasoning_tokens`；可能带 `billing_mode=credits` 与 `cn_credits_remain_info` | 挂到后续 chunk 的 `usage`。**SOLO 真余额是这里的 `ide_credits`，不是权益包合计** |
| `done` | `finish_reason` | 空 delta + finish，然后 `[DONE]` |
| `error` | `code`, `message` | 内部 error 事件，再 `[DONE]` |

非流式：拼 `response` / reasoning，按 index 合并 tool_calls。`mergeToolCalls` 可抄 WorkBuddy 的合并算法，不要共享 OpenAI-chunk 解析。

排队：Solo 路径用业务码限流，而不是旧 IDE 的 `request_wait_in_queue`。不要把「排队中」写进 `content`。

### 积分 / 签到

| 接口 | 路径 | 用途 |
|------|------|------|
| 权益包 | `POST {UgHost}/trae/api/v2/pay/ide_user_ent_usage` | 包合计；2000 余量常常是 `work_credits`，**不能当 SOLO 余额，不能当选号依据** |
| 签到状态 | `POST {UgHost}/trae/api/v2/ug/checkin_credits/status` | display |
| 签到领取 | `POST {UgHost}/trae/api/v2/ug/checkin_credits/claim` | 可选，默认关，对标 WorkBuddy 签到开关 |
| SSE `ide_credits` | chat 过程中 | 唯一比较接近 SOLO 真余额的信号 |

`Quota`：

- 先可返回权益包（unit 标明「权益包，非 Solo 余额」），失败返回 `nil`，**永远不改 Ready**
- 不要按剩余积分选号
- 自动签到默认关

### 错误分类

| 上游 | 含义 | 本仓库 `ClassifiedError` |
|------|------|--------------------------|
| `1001` / 401 | 登录态死 | `kind=auth`，禁用/要求重登，failover |
| `1005` | 套餐不够 | `kind=quota`，**长冷却（约 12h）**，failover |
| `4001` | 模型/版本头不匹配 | `kind=invalid_request`，不换号、不冷却账号 |
| `4008` | SOLO 额度耗尽 | `kind=quota`，等到重置/签到；短重试无意义，中长冷却 |
| `4011` | 硬限流 | `kind=rate_limit`，failover + 冷却 |
| `429` | 软限流 | `kind=rate_limit`，约 60s 冷却，尊重 Retry-After |
| `9074` | 签到拥挤 | 只影响签到，不影响 chat Ready |
| 5xx | 上游挂 | `kind=unavailable`，failover + 短冷却 |
| 其它 4xx | 请求问题 | `kind=invalid_request` 或 `unavailable`；不要把所有 4xx 当 quota |

adapter 内不要 `sleep` 重试。冷却和换号交给 executor。

## 哪些代码可以直接用

### 本仓库：原样复用（不要为 Trae 新开调度/进程模型）

| 位置 | 用法 |
|------|------|
| `internal/providers/interfaces.go` 的 `Adapter` 全套接口 | Trae Client 实现同一组能力，`Adapter()` 打包 |
| `internal/providers/registry.go` `ProviderDescriptor` / `Resolve` | 加 `trae` 描述符；`region=cn` |
| `internal/providers/registry_runtime.go` `Register` / `Get` | `internal/api/server.go` 里 `workbuddy.NewClient(store)` 旁边再 `Register` 一次 |
| `internal/accounts` SQLite + `LoadCredentialPayload` / `SaveCredentialPayload` | 存 `trae-oauth-v1`；WorkBuddy `Store` 接口可原样抄到 Trae Client |
| `internal/executor/chat.go` round-robin + pin + 冷却 + failover | Trae 账号作为 in-process 候选自然进入；不改调度哲学 |
| `internal/api/accounts.go` `login/device` + `login/status` | Trae 走现有 Start/Poll；callback 藏在 adapter 内 |
| `internal/api/accounts.go` import/export 分发 | `ImportExporter` 接 nested/flat JSON |
| `internal/translate.ChatRequest` / `ChatOutcome` | 边缘把 Solo SSE 转成内部契约 |
| 控制台 `AddAccountModal` 的 provider 选择器 | 加 `trae` + `cn` + i18n；卡片复用 UID / 就绪 / 冷却 |
| WorkBuddy 的「catalog 失败即错、Quota 只展示、签到默认关」产品决策 | 原样沿用 |

WorkBuddy 文件切分直接当模板：

```
internal/providers/workbuddy/
  client.go credential.go headers.go payload.go sse.go client_test.go
→
internal/providers/trae/
  client.go credential.go headers.go payload.go sse.go client_test.go
```

`workbuddy/client.go` 的 `do()`、token 刷新锁、`Probe`/`Quota` 分家、`Adapter()` 打包，结构可以几乎照抄。

### 本仓库：只能抄算法，不能共享文件

| 代码 | 原因 |
|------|------|
| `workbuddy/sse.go` `Aggregate` | 它认 OpenAI `data:` chunk；Solo 是 `event: output/done/error` |
| `workbuddy/payload.go` `PrepareBody` | WorkBuddy 只强制 stream / tool_choice / 补空 system；Solo 还要 `function`、`config_name`、content 数组、parameters 字符串化 |
| `workbuddy/headers.go` | 头名和 JWT scheme 完全不同（`Cloud-IDE-JWT` vs `Bearer`） |
| `workbuddy/credential.go` 字段 | 多了 `machine_id` / `device_id` / `api_host` / 双 expire |

tool_calls 按 index 合并的循环可以从 WorkBuddy `sse.go` 抄一份进 `trae/sse.go`。等两个 provider 都稳了再考虑抽到 `internal/translate`，现在不要提前抽象。

### trae2api-web：重写成 `internal/providers/trae/`（只借协议）

对照提交，不要 `vendor`、不要复制它的 `cmd/` / `internal/server/admin.html` / `internal/pool`。

| 参考文件 | 借什么 | 落到 |
|----------|--------|------|
| `internal/upstream/constants.go` | host、AppID、ClientID、版本、path | `credential.go` 常量 |
| `internal/upstream/headers.go` | SOLO / Ug / OAuth 头 | `headers.go` |
| `internal/upstream/payload.go` | OpenAI → `solo_work_lite` 改写 | `payload.go` |
| `internal/upstream/solosse.go` | 事件解析与 OpenAI 映射 | `sse.go` |
| `internal/upstream/client.go` | ExchangeToken、GetUserInfo、ChatStream、FetchModels、Classify、权益包/签到 | `client.go` |
| `internal/auth/auth.go` | nested/flat JSON、提前刷新、原子写回 | `credential.go`（写 SQLite 不是写 `trae-{uid}.json`） |
| `internal/server/login.go` + `callback.go` | 授权 URL、pending、callback 解析 | `client.go` 的 StartLogin/PollLogin |
| `docs/RESEARCH.md` | 通道边界、错误码、`ide_credits` vs 权益包 | 本文；实现时当注释约束 |

### 明确不用

- trae2api-web 整仓、Docker 入口、`/admin`、`auths/` 文件池、`data/state.json`
- 按剩余积分选号、连续失败阈值那套独立 pool（本仓库已有 cooldown）
- 默认自动签到
- `linqiu919/trae2api` 的 `user_input` / `chat_history` / `CODING_TOKEN` / 随机设备 / 硬编码 Claude 映射 / `request_wait_in_queue`
- 官方 `traecli` 二进制、每请求 `--print`、WASM hook
- Redis、第二套 API key、新监听端口

## 目标架构

```
OpenAI 客户端
   │  POST /v1/chat/completions   (本仓库 API key)
   ▼
internal/api/chat.go  ──► internal/executor/chat.go
                               │  pick 就绪账号（含 trae）
                               ▼
                    providers.Registry.Get("trae")
                               ▼
                  internal/providers/trae/   (纯 Go HTTP/SSE)
                               │  ExchangeToken + SOLO 头
                               │  POST /api/agent/v3/llm_utils_chat
                               ▼
                  trae-api-cn.mchost.guru  (solo_work_lite)
```

`internal/providers/trae/` 只负责 Trae 协议。executor 不感知 Solo 细节。translate 不出现 `solo_work_lite`。

### Provider descriptor

```go
ProviderDescriptor{
    ID:            "trae",
    Label:         "Trae",
    Runtime:       RuntimeInProcess,
    AuthTypes:     []AuthType{AuthOAuth},
    CredentialFormats: []string{"trae-oauth-v1"},
    Capabilities: ProviderCapabilities{
        Chat:true, Stream:true, Tools:true, Images:false,
        Reasoning:true, ModelCatalog:true, Usage:true, // usage 来自 SSE token_usage
        Login:true, BrowserLogin:true, PATLogin:false, ImportExport:true,
    },
    Regions: []RegionDescriptor{
        {ID:"cn", Label:"CN Solo",
         ChatBase:"https://trae-api-cn.mchost.guru",
         BillingBase:"https://api.trae.cn",
         AuthBase:"https://api.trae.com.cn",
         DefaultDomain:"trae.cn"},
        // 国际版 IDE / 企业 CLI 以后若做：加 region，不要新建 traecn / traecli family
    },
    DefaultRegion: "cn",
}
```

控制台显示「Trae 国内版（Solo）」。提交仍是 `provider=trae` + `region=cn`。

## 数据模型

不新增 SQLite 列。`credential_payload` 存：

```json
{
  "format": "trae-oauth-v1",
  "access_token": "...",
  "refresh_token": "...",
  "expires_at": 1710000000,
  "refresh_expires_at": 1710000000,
  "domain": "trae.cn",
  "api_host": "https://api.trae.com.cn",
  "uid": "...",
  "enterprise_id": "...",
  "nickname": "...",
  "machine_id": "<32 hex, 持久化>",
  "device_id": "<32 hex, 持久化>",
  "ide_version": "0.1.52",
  "ide_version_code": "20260811"
}
```

`DecodeCredential` 同时接受 trae2api-web 的 nested `{auth, account}`（`accessToken` / `machineId`）和本仓库扁平 snake_case。device 指纹一旦写入就不再随机刷新。

## 详细实施计划（T0–T5）

前置：WorkBuddy 真实账号验收通过，或显式 defer 当前 Qoder 里程碑。阶段号接在 WorkBuddy J0–J4 之后。

### T0 — 控制面承认 `trae`

- [x] `registry.go` 注册上面的 descriptor
- [x] accounts store 的 provider/region 校验接受 `trae` + `cn`（改 `TestStoreRejectsUnknownProviderAndRegion` 同类测试）
- [x] `internal/api/server.go`：`trae.NewClient(store)` + `Register`
- [x] 前端 `AddAccountModal` + i18n `accountTypeTraeCN`

### T1 — 凭证与登录

- [x] `credential.go`：Validate / Encode / Decode（nested + flat）、持久化 device
- [x] `ExchangeToken` + 提前刷新 + 刷新失败标未就绪
- [x] `StartLogin`：持久化或复用 device、起 `127.0.0.1` callback、返回授权 URL
- [x] `CompleteLogin`：浏览器和服务不在同一台时，可粘贴完整 `127.0.0.1/authorize?...` URL
- [x] `PollLogin`：pending → ExchangeToken → GetUserInfo → SaveCredentialPayload
- [x] Import/Export 兼容 `trae-{uid}.json`
- [x] 不绑公网、不提供 CODING_TOKEN、不每轮换设备

### T2 — Chat 最小闭环

- [x] `payload.go`：按 Solo 规则改写
- [x] `headers.go`：SOLO / Ug / OAuth 三套头
- [x] `sse.go`：output / done / error / token_usage → `ChatOutcome` / 流式 relay
- [x] `ChatStream` 返回 `*http.Response`；`ChatNonStream` 聚合
- [x] httptest：非流式 tools + reasoning；流式 `[DONE]`

### T3 — 模型、错误、Probe

- [x] `Models`：`get_detail_param`，空/失败显式错误
- [x] `Classify`：上表 1001/1005/4001/4008/4011/429
- [x] `Probe`：凭证就绪即热；刷新失败标未就绪
- [x] `Quota`：权益包 display-only，unit=`entitlement_pack`；失败 `nil`

### T4 — 池质量（可选，紧跟 T2）

- [x] 签到 API 已实现，默认不自动领取
- [x] SSE `ide_credits` 不覆盖权益包、不进选号
- [x] `4008` / `1005` 冷却时长用契约测试钉住

### T5 — 验收

- [ ] 真实消费级 Trae CN 账号：`只回复OK`、catalog 列表含实测模型、failover、工具调用（用户验收）
- [x] 无企业旗舰号时 **不要** 用 `traecli` 冒充验收
- [ ] 同名模型进 Route Pool；开关位于控制台「系统设置」，默认开启

## 建议的产品决策（实现前锁定）

1. 只做 `provider=trae` + `region=cn` 的 Solo 通道。
2. 只借 `trae2api-web` 协议，不嵌它的网关。
3. 不 spawn `traecli`，不 hook 官方二进制。
4. 正规浏览器登录或 JSON 导入；禁 `CODING_TOKEN`。
5. device 指纹按账号持久化。
6. 动态 `get_detail_param`，失败不猜模型表。
7. 钉 `IdeVersion=0.1.52` / `20260811`，升级要显式改常量并回归 catalog。
8. 调度仍是 round-robin + pin + failover；积分只展示。
9. 自动签到默认关。
10. callback 只绑 `127.0.0.1`。
11. 里程碑前置：T0–T5 排在 WorkBuddy 真实验收之后，或显式 defer。

## 风险

1. **上游私有协议**：Solo 不是公开 SDK。缓解：版本头钉死；catalog / chat 契约测试；失败显式报错。
2. **权益包 ≠ Solo 余额**：按包选号会把 Work 积分和 SOLO 漏斗混在一起。缓解：不按积分调度。
3. **登录必须本机 callback**：远程浏览器贴 URL，不要开公网端口。
4. **企业 CLI 诱惑**：有人会想 pin `traecli`。缓解：本文和 `AGENTS.md` 写死禁止 spawn 完整 CLI。
5. **多模态未核实**：`Images` 默认 false，数组 content 保守透传。

## 对本仓库可用性的判断

控制面扩展点已经够用。Trae CN Solo 真正要写的是 `internal/providers/trae/` 一个包 + registry 一行 + server Register 一行 + 前端一个选项。协议事实以 `trae2api-web` 为准；旧国际版 IDE 调研只作对照，不再指导实现。
