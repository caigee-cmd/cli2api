# 多上游账号类型：WorkBuddy 对照与扩展计划

last-updated: 2026-08-28
status: WorkBuddy J0–J4 已落地；Qoder CN 代码完成，L6 真实账号验收未完成
routing-reference: [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) `main` @ `dc3c3b1`
workbuddy-reference: [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api) `master` @ `92514d8ba06413c3b620e96da3ebc38e6c7beda0`

本文回答三件事：

1. WorkBuddy / CodeBuddy 能不能作为本仓库的新账号类型。
2. 参考它的代码后，当前仓库的架构是否够用、哪里必须先改。
3. 若要做，完整怎么做，以及后面 Cursor 等上游怎么接。

当前仍以 Qoder 里程碑为准。H7 验收和 Phase I 协议边界未完成前，不实现 WorkBuddy 流量。硬规则见 `AGENTS.md`，Qoder 运行时见 `docs/DESIGN.md`，当前清单见 `docs/PLAN.md`。

## 结论

**可以参考，不能当 drop-in 账号类型。**

| 问题 | 答案 |
|------|------|
| 值不值得做成新 `provider` | 值得。它是「自有登录态 → OpenAI 兼容 API」的同类网关，和本仓库产品形态接近。 |
| 现在能不能直接加进 Accounts | 不能。SQLite、凭证、登录、执行器、模型目录、进程模型和调度信号全部绑死 Qoder。 |
| 现有仓库能不能扩展到多上游 | 能，但要先把控制面从「Qoder 账号表」改成「带 provider 的账号注册表」。前端已有类型选择器，后端还没有。 |
| 该不该整仓移植 workbuddy2api | 不该。只借协议、登录、错误分类和积分信号；不借它的文件账号池、容器重启加载、静态模型表和按积分选号。 |
| WorkBuddy 要不要 Node / WASM worker | 不要。它是纯 HTTP/SSE + Bearer token，没有进程内全局 AuthManager。应走 Go in-process adapter。 |
| 同名模型能不能跨 provider 池化 | 能，但必须做成显式 Route Pool：同一个 public model ID 下的 provider / native model / account 候选集合，不能把所有账号混成一个无约束池。 |
| 和 Cursor / Anthropic 的关系 | 同一条扩展轴。WorkBuddy 是第一个非 WASM 上游；Cursor 仍然更晚、协议更脏。 |

一句话：本仓库已经有「控制面 + 执行面」雏形，但执行面还不是插件。WorkBuddy 适合作为第一个 in-process provider，用来验证扩展点，而不是把另一套网关嵌进来。

## WorkBuddy 是什么

WorkBuddy2API 把 **WorkBuddy CN / CodeBuddy**（`copilot.tencent.com` / `www.codebuddy.cn`，国际站 `www.workbuddy.ai`）的插件 OAuth 登录态，转成本地 OpenAI 兼容接口。

它和本仓库的相似点：

- 自托管，单 API key
- 多账号轮转、冷却、failover
- OpenAI `POST /v1/chat/completions` + SSE
- 工具调用、`reasoning_content`
- 设备授权登录，而不是让用户手工贴长效 key

它和本仓库的本质差异：

| 维度 | CLI2API（本仓库） | workbuddy2api |
|------|-------------------|---------------|
| 上游 | Qoder cloud + 本地 pinned `qodercli` WASM | CodeBuddy / WorkBuddy HTTP API |
| 运行时 | Go 控制面 + 每账号一个 Node daemon | 纯 Go，无子进程 |
| 凭证 | 加密 `.auth/user` blob + `machine_id` | `accessToken` / `refreshToken` / uid / domain |
| 登录 | 浏览器 device-flow / PAT / `qoder-native-v1` 导入 | `POST /v2/plugin/auth/state`，无 PKCE，服务端发 state |
| 对话入口 | worker 内 `prepareInferRequest` → `agent_chat_generation?Encode=1` | `POST {chatBase}/v2/chat/completions` |
| 模型目录 | 登录后的 Qoder in-process catalog，失败即空 | `GET /console/enterprises/personal/models`，失败回退静态表 |
| 账号存储 | SQLite + 派生 runtime HOME | `auths/workbuddy-<uid>.json` + `state.json` |
| 调度 | Go round-robin + pin + inflight + 错误分类 | 剩余积分最高者优先 |
| 产品附加 | 控制台、托管更新、Anthropic 协议规划 | 每日签到、积分查询、token keepalive |
| 控制台 | React / HeroUI | 无 |

参考价值高的是 **OAuth、token 刷新、chat 头、SSE、错误分类、积分/签到**。参考价值低的是账号落盘方式和进程模型。

## 上游协议（从参考仓库抽出）

### 登录

无 PKCE。state 由上游签发。

1. `POST https://copilot.tencent.com/v2/plugin/auth/state?platform=CLI` 空 JSON
2. 返回 `{ state, authUrl }`，浏览器打开 `authUrl`
3. `GET /v2/plugin/auth/token?state=` 一次；未完成时业务 `code != 0`（`login ing`）
4. 成功后带 Bearer 调 `GET /v2/plugin/login/account?state=` 拿 `uid` / `enterpriseId` / `nickname`
5. 落盘：

```json
{
  "account": { "uid": "...", "enterpriseId": "...", "nickname": "..." },
  "auth": {
    "accessToken": "...",
    "refreshToken": "...",
    "expiresAt": 1710000000,
    "domain": "codebuddy.cn"
  }
}
```

也兼容扁平 JSON。`domain` 含 `workbuddy.ai` 视为 global，否则 CN。

刷新：`POST {chatBase}/v2/plugin/auth/token/refresh`，头里带 `X-Refresh-Token`。chat 请求禁止带这个头。`12153` / `Offline user session not found` 视为 session 死亡，账号应禁用并要求重登。

### Chat

- URL：CN `https://copilot.tencent.com/v2/chat/completions`；global `https://www.workbuddy.ai/v2/chat/completions`
- 上游拒绝非流式，发出前强制 `stream: true`；对本仓库客户端的非流式请求，应在适配器内聚合 SSE
- `tool_choice` 必须是 string。对象形式会 `400 code=11101`
- 关键头：`Authorization`、`X-User-Id`、`X-Enterprise-Id` 或 `X-No-*`、`X-Product: SaaS`、`X-Domain`、UA `CLI/2.139.0 CodeBuddy/2.139.0`
- Chat 另加官方 CLI 通道头（CN/Global 同名，Origin/host 仍按区域分开）：`X-IDE-Type/Name/Version`、`X-Agent-Type/Intent`、`X-Request-ID` / conversation/session IDs、`X-Product-Version`、`X-Private-Data: false`。Refresh 不带这些头，也绝不带 `X-API-Key`
- SSE 已是 OpenAI chunk 形态，含 `delta.content`、`delta.reasoning_content`、按 index 合并的 `tool_calls`

### 模型

`GET {chatBase}/console/enterprises/personal/models`

只暴露 `agents[].name == "cli"` 的模型 ID，再和 `data.models` 交叉，跳过 `disabled`。字段用 `maxInputTokens` / `maxOutputTokens`，不是 `contextWindow`。

参考仓库失败时回退静态表。本仓库对 Qoder 的原则是 **catalog 失败就报错，不猜**。WorkBuddy 应沿用同一原则：动态目录失败返回明确错误，不内置一份会过期的模型白名单。

### 积分与签到

- 余额：`POST {billingBase}/v2/billing/meter/get-user-resource`
- 签到：`POST {billingBase}/v2/billing/meter/daily-checkin`
- CN billing host 是 `https://www.codebuddy.cn`，和 chat host 不是同一个
- 参考仓库 09:00 / 21:00 自动签到，22:00 refresh keepalive
- 选号按剩余积分降序

这些是 WorkBuddy 产品能力，不是本仓库必须复制的调度哲学。本仓库已有 round-robin、pin、`max_inflight`、quota/rate_limit/auth 分类。积分只应作为 **可选的 provider 信号**，不能替换总调度器。

### 错误分类对照

| WorkBuddy | 本仓库 taxonomy | 处理 |
|-----------|-----------------|------|
| `hard_credit` / 402 / 「积分不足」 | `quota` | 同 provider family 内 failover；跨 provider 只在显式开启同名 Route Pool 后，于同一 public model ID 内 failover |
| `soft_rate` / 429 | `rate_limit` | failover + 短冷却，尊重 Retry-After |
| `session_dead` / 401 + 12153 | `auth` | 禁用或要求重登，failover |
| 404 偶发 | `unavailable` | 短冷却；参考仓库不累计 errCount，可吸收 |
| 5xx | `unavailable` | failover + 短冷却 |
| 其他 4xx / 业务 code | `unavailable` 或保留 provider code | 不要把所有 4xx 当 quota |

不要把 WorkBuddy 的 `ErrKind` 直接塞进 SQLite。继续写本仓库的 `last_error_kind`，在 WorkBuddy adapter 里做映射。

## 当前仓库的真实扩展性

### 已经可复用

这些层不需要为 WorkBuddy 重写：

- 控制台 API key 门禁、`/health` 公开
- SQLite 账号注册表、enable/disable/delete、cooldown 持久化
- Go 唯一调度器：round-robin、`X-Qoder-Account` pin、inflight、failover
- OpenAI `POST /v1/chat/completions` 入口和 SSE 中继
- `translate.ChatRequest` 已是 OpenAI 形态；WorkBuddy 上游也是 OpenAI 形态，翻译负担小于 Qoder
- 错误 taxonomy 和 console Accounts 表格
- 单容器、`/data`、托管更新
- 前端 `AddAccountModal` 已有 `AccountType` 下拉雏形，可演进为由 descriptor 生成 `provider + region`

### 现在还不能扩展的点

前端已经在演「多账号类型」，后端会把 `provider / region / credential format` 丢掉。

| 位置 | 现状 | 对 WorkBuddy 的影响 |
|------|------|---------------------|
| `accounts` 表 | 无 `provider` / `region` 列 | 无法区分上游与区域 |
| `account_credentials` | 只有 `user_blob` + `machine_id` | WorkBuddy token JSON 存不进去 |
| `Store.Create` / `handleAccounts` POST | 忽略 `provider` / `region` | 控制台选出的类型不会落库 |
| `Manager.startAccount` | 一律物化 `.qoder/.auth` 并拉起 Node daemon | WorkBuddy 会被错误地当成 Qoder worker |
| `ExecStarter` | 固定 `QODER_HOME` / `qodercli` | 非 Qoder 账号无法启动 |
| `executor/chat.go` | 只 POST worker `/v1/chat/completions` | 没有 in-process 上游 |
| `fetchWorkerModels*` | 只问 Qoder daemon catalog | WorkBuddy 模型进不了 `/v1/models` |
| `handleAccountImport` | 只接受 `qoder-native-v1` | 不能导入 workbuddy JSON |
| login 代理 | 一律转到 worker `/admin/login/*` | WorkBuddy 没有这个 worker |
| Phase I 文档 | canonical conversation 只通向 Qoder adapter | 多上游时这条图是错的 |
| `Pool.Item` | 只有 URL，没有 provider family / route capability | 调度器不知道账号家族与模型能力 |
| 模型 ID | 全局小写 public ID | Qoder 和 WorkBuddy 都可能出现 `glm-5.2`，会撞名 |

### 架构判断

当前结构是：

```text
Client
  -> Go auth / translate / api
    -> accounts.Pool
      -> 每个 enabled 账号一个 Node daemon
        -> Qoder WASM encode + cloud SSE
```

这是 **单上游、多账号、进程隔离**。Qoder 必须如此：WASM / AuthManager 进程内全局，不能共享。

WorkBuddy 需要的是：

```text
Client
  -> Go auth / translate / api
    -> accounts.Pool
      -> in-process WorkBuddy client（每请求带该账号 token）
        -> copilot.tencent.com / workbuddy.ai SSE
```

同一套调度器可以共用，同一套执行器不能共用。扩展性取决于能不能把「账号」和「运行时」拆开。拆开之后，Cursor 也可以作为第三种 runtime，而不用再挖一遍控制面。

## 目标架构

保持现有分层：`auth / endpoint / executor / translate / api`。新增的是 **provider adapter**，不是第二套服务。

```text
/v1/chat/completions  -> OpenAI ingress
/v1/messages          -> Anthropic ingress        # Phase I，独立里程碑
                              |
                      canonical conversation
                              |
              provider router（看账号 provider，不看 URL 路径）
                     /                \
            Qoder adapter         WorkBuddy adapter
         Node daemon + WASM      Go HTTP/SSE + token
                     \                /
                      accounts.Pool（唯一调度器）
```

规则：

- 公共协议适配器不知道 Qoder WASM，也不知道 WorkBuddy 的 `X-User-Id`
- Qoder adapter 继续独占 pinned `qodercli` 兼容
- WorkBuddy adapter 独占 token、刷新、强制 stream、`tool_choice` 字符串化、CN/global host
- Go 仍然是唯一的挑号、冷却、pin、failover 所有者
- 多账号隔离策略按 runtime kind 决定，不按「所有账号都必须有 worker」决定

### Provider descriptor 与能力接口

不要把「上游产品、区域、运行时、登录方式、凭证格式」压成一个字符串。第一版仍然静态编译，不做动态插件加载，
但控制面只依赖 descriptor 和小接口，避免 `manager.go` / `chat.go` 出现 provider 特例。

```go
type ProviderDescriptor struct {
    ID                string // qoder / workbuddy
    Regions           []RegionDescriptor
    Runtime           RuntimeKind // child_process / in_process
    AuthTypes         []AuthType
    CredentialFormats []string
    Capabilities      ProviderCapabilities
}

type RegionDescriptor struct {
    ID       string // global / cn
    ChatBase string
    BillingBase string
}

type ProviderCapabilities struct {
    Chat         bool
    Stream       bool
    Tools        bool
    Images       bool
    Reasoning    bool
    ModelCatalog bool
    Usage        bool
    Login        bool
    ImportExport bool
}
```

建议拆成小接口，而不是一个巨型 `Provider` 接口：

| 接口 | 职责 | 可选性 |
|------|------|--------|
| `CredentialCodec` | validate / decode / encode credential payload | 必须 |
| `LoginSessionProvider` | start / poll / cancel 登录流 | 支持浏览器登录时必须 |
| `ChatExecutor` | non-stream / stream 执行 | 必须 |
| `ModelCatalogProvider` | 拉取账号可用模型与能力 | 必须 |
| `AccountProber` | refresh / health / usage / quota | 可选 |
| `ErrorClassifier` | provider error -> internal taxonomy | 必须 |
| `ImportExporter` | provider 专属导入导出 | 可选 |

规则：

- `provider` 是 family：`qoder`、`workbuddy`；`region` 是独立属性：`global`、`cn`
- `runtime` 由 descriptor 声明，不落库、不由 console 猜
- `credential_format` 独立于 provider；一个 provider 未来可以有多个格式版本
- 控制台表单、登录向导、能力展示从 descriptor 生成，不硬编码 WorkBuddy 分支
- SQLite 只存通用账号字段和 credential payload，不添加 provider 专属业务列
- 未实现的能力接口返回显式 `unsupported`，不让调用方猜 nil
- `ProviderCapabilities` 表示 provider 产品级能力；`RouteTarget.Capabilities` 表示某个模型的实际能力，两者不能混用

这个抽象的目标不是一步做插件系统，而是让 Cursor / 其他上游接入时只需要注册新的 descriptor 与 adapter。

初始 provider matrix：

| Provider family | Runtime | Auth | Credential format | Login | Catalog |
|-----------------|---------|------|-------------------|-------|---------|
| `qoder` + `global` | `child_process` | OAuth / PAT / native import | `qoder-native-v1` | worker + `@qoder-ai/qodercli` | worker in-process catalog |
| `qoder` + `cn` | `child_process` | OAuth / PAT / native import | `qoder-native-v1` | worker + `@qodercn-ai/qoderclicn` | worker in-process catalog |
| `workbuddy` | `in_process` | OAuth | `workbuddy-oauth-v1` | Go state + poll | WorkBuddy models endpoint |
| future `cursor` | TBD | TBD | TBD | TBD | TBD |

### Runtime kind

| provider family | runtime | 为什么 |
|-----------------|---------|--------|
| `qoder` | `child_process` | WASM / AuthManager 进程全局；一 HOME 一 daemon。`region=global` 与 `region=cn` 是两套 pinned CLI，不能共享进程 |
| `workbuddy` | `in_process` | 凭证是普通 Bearer；并发安全由 per-account mutex 保护 refresh |
| 未来 `cursor` | 先按调研再定，大概率 `in_process` 或独立 daemon；不要假设等于 Qoder | |

不要为 WorkBuddy 启动 `worker/src/daemon.mjs`。也不要让 Qoder 账号改走 in-process HTTP——Qoder 没有稳定的明文 chat 入口可替代 WASM encode。

### 账号标识

建议拆开保存：

- `provider = qoder`，`region = global`：现有 Qoder 行为；历史空值按它读
- `provider = workbuddy`，`region = cn/global`：host 由 descriptor 与 credential domain 共同决定

不要把 region 塞进 provider family。region 影响 host 与凭证 domain，但不应该变成两套 adapter、两套接口或两个控制面分支。

控制台类型选择器显示 `Qoder Global`、`WorkBuddy CN`、`WorkBuddy Global`，但提交时保留 `provider` 与 `region` 两个字段。后端必须持久化，不能再丢字段。

Pin 头短期仍用 `X-Qoder-Account`（兼容现有客户端）。内部改叫 account id。若加 `X-CLI2API-Provider`，只作为过滤，不替代账号 pin。

## 数据模型

新增不可变 migration，例如 `003_account_providers.sql`。不要改 `001` / `002` 的 checksum。

```sql
ALTER TABLE accounts ADD COLUMN provider TEXT NOT NULL DEFAULT 'qoder';
ALTER TABLE accounts ADD COLUMN provider_region TEXT NOT NULL DEFAULT 'global';

CREATE TABLE IF NOT EXISTS account_credential_payloads (
  account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  format TEXT NOT NULL,
  payload BLOB NOT NULL,
  updated_at TEXT NOT NULL
);
```

过渡策略：

- 现有 `account_credentials.user_blob/machine_id` 继续服务 Qoder，不强制一次迁移
- Qoder 的 provider/region 分别为 `qoder` / `global`；历史空 provider 按此读取
- WorkBuddy 走 `account_credential_payloads`，`format = workbuddy-oauth-v1`
- 读取时按 `accounts.provider + credential_format` 选择 codec；不要按 provider 硬编码表结构
- `/api/accounts` 返回 `provider` 与 `region`，永不返回 raw token；导出走显式 export

`workbuddy-oauth-v1` payload：

```json
{
  "access_token": "",
  "refresh_token": "",
  "expires_at": 0,
  "domain": "",
  "uid": "",
  "enterprise_id": "",
  "nickname": ""
}
```

`auth_type` 建议：

- Qoder：`none` / `oauth` / `pat` / `native`（保持）
- WorkBuddy：`none` / `oauth`；导入已有 JSON 也记 `oauth`

禁止：

- 把 WorkBuddy token 写进 `.qoder/.auth/user`
- 用 `auths/*.json` 目录替代 SQLite
- 用 `state.json` 再做一套冷却存储

## 登录与控制台

WorkBuddy 登录应进现有 Accounts 向导，而不是 `login.sh` + `docker restart`。

向导按类型分叉：

| 类型 | 浏览器 | PAT | 导入 |
|------|--------|-----|------|
| Qoder Global | 现有 device-flow → worker | 现有 PAT → worker | `qoder-native-v1` |
| WorkBuddy CN/Global | 本进程 `auth/state` + poll | 不提供（上游是 OAuth） | `workbuddy-oauth-v1` JSON |

WorkBuddy 浏览器流：

1. `POST /api/accounts` 带 `provider` 与 `region`
2. `POST /api/accounts/:id/login/device` 由 Go WorkBuddy adapter 调上游，返回 `authUrl`
3. 控制台打开 URL，轮询 `GET /api/accounts/:id/login/status`
4. 成功后写 SQLite，更新 `remote_uid` / `auth_type=oauth`
5. in-process runtime 标记 ready；**不 spawn Node，不重启容器**

导入校验：必须有 `accessToken`（或嵌套 `auth.accessToken`）以及 `uid`。缺 uid 时允许登录后补，但不能当 ready。

导出：显式动作，格式 `workbuddy-oauth-v1`，不含控制台 API key。

健康展示：WorkBuddy 没有 WASM `hot`。用 `ready = token 有效且最近 refresh/probe 成功`。积分可显示为次要指标，不代替 ready。

## 执行路径

`executor` 按账号 provider 分支，而不是按「有没有 worker URL」。

```text
ChatExecutor.Pick(...)
  -> item.Provider == qoder
       -> 现有 HTTP 到 Node daemon
  -> item.Provider == workbuddy-*
       -> 如需则 refresh
       -> PrepareBody（stream=true，tool_choice 字符串化）
       -> ChatStream
       -> 客户端要 stream：透传 SSE
       -> 客户端不要 stream：Aggregate
```

Qoder 路径一行都不应该为了 WorkBuddy 改 payload 形状。WorkBuddy 的 `PrepareBody` 只活在自己的 adapter 里。

`Pool.Item` 增加：

- `Provider string`
- `Runtime string`：`child` / `inprocess`
- 可选 `Credits int64`、`Nickname string`，仅 WorkBuddy 填充

挑号默认保持全局 round-robin。不要把「积分最高者优先」做成总策略。若以后要按积分挑 WorkBuddy 号，也只在 `provider=workbuddy-*` 的候选子集里做。

模型列表：

- `/v1/models` 返回 **已登录账号目录的并集**
- 每条带 `owned_by` / `provider`
- 默认保守模式：`glm-5.2` 继续给 Qoder（现网兼容）；WorkBuddy 用 `workbuddy/glm-5.2` 或账号 pin
- 显式 Route Pool 模式：`glm-5.2` 表示同名 Qoder / WorkBuddy 候选池；`qoder/glm-5.2` 与 `workbuddy/glm-5.2` 仍可强制 provider
- 不要做跨上游的手写 alias 表

## 同名模型跨 Provider Route Pool

目标：允许 `glm-5.2` 这类 public model ID 同时路由到 Qoder 与 WorkBuddy 的同名模型，但必须显式开启，
不能静默把新 provider 混入现网模型。

### 术语

- **Public model ID**：客户端请求和响应使用的稳定 ID，例如 `glm-5.2`
- **Route target**：一个可执行候选，包含 provider family、native model、account ID 和能力
- **Route pool**：同一个 public model ID 下全部可用 route target
- **Provider family**：调度聚合单位；`qoder`、`workbuddy`。region 不参与 provider 层调度

```go
type RouteTarget struct {
    PublicModel  string
    Provider     string
    NativeModel  string
    AccountID    string
    Capabilities ModelCapabilities
}

type RouteQuery struct {
    PublicModel    string
    PreferAccount  string
    ProviderFilter string
    Excluded       map[string]struct{}
}
```

### 开关与 ID 规则

- `cross_provider_model_pool` 默认关闭，先保持现网 bare ID 行为不变
- 关闭时：bare ID 留给既有 Qoder；WorkBuddy 使用 provider 前缀或账号 pin
- 开启后：bare ID 变成同名 route pool；provider 前缀仍可用于强制单一 provider
- 匹配必须精确；不做模糊别名、字符串相似或手写 alias 表

### 路由与调度

Model route registry 从各账号动态目录构建，初始只需内存快照，不新增 SQLite 路由表：

```text
public model id
  -> ModelRouteRegistry
       provider family -> native model -> enabled accounts
  -> route-aware accounts.Pool
       provider family round-robin
       account round-robin
       cooldown / inflight / pin / capability filter
  -> provider adapter
```

规则：

- 不新增第二套调度器；把现有 `accounts.Pool` 升级为 route-aware scheduler
- provider family 先 round-robin，避免账号更多的 provider 独占流量
- provider family 内再按账号 round-robin，继续复用 cooldown、inflight、priority、pin
- 调度游标按 `public model -> provider family` 与 `public model + provider family -> account` 维度维护
- 账号 pin 优先；pin 不存在、不可用或不支持该模型时直接报错，不静默换号
- capability 不满足的 route target 先过滤，例如 context、tools、images、reasoning
- failover 只发生在同一个 public model ID 的 route pool 内
- 参数错误、模型不存在、能力不足不 failover
- streaming 只有在第一个输出字节前才允许重试；已经开始输出则不能重放

### 能力、设置与响应

- route target 必须携带 provider 专属 context / max tokens / tool / image / reasoning 能力
- `model_settings` 需要从全局 `model_id` 演进为 provider-scoped 设置；同名池不能让 Qoder 与 WorkBuddy 共用一个冲突 context 值
- 响应中的 `model` 必须重写回客户端请求的 public model ID
- 响应可带 `X-CLI2API-Provider` 便于观测；账号仍用现有 pin / account header 语义
- usage / credits 只记录实际 provider，不能把 Qoder credits 与 WorkBuddy 积分相加或比较

### CLIProxyAPI 参考结论

CLIProxyAPI 的可借鉴点是：

- ModelRegistry 按 model ID 聚合 provider，并保存 provider 专属模型信息
- 请求先解析出所有可用 provider，再交给 provider/model shard 调度
- provider 与账号分层轮转，并支持 cooldown / priority / weight

不直接照搬的点：

- CLIProxyAPI 对重叠 alias 依赖配置约束；本仓库要显式 route pool 开关和 deterministic 路由规则
- 不默认把 provider 数量、剩余积分或账号数量当作 provider 权重
- 第一版不做插件 ModelRouter，只做内置 route registry 与 route-aware pool

## 调度与后台任务

可从参考仓库吸收、但要改写到现有 pool：

- token 临近过期先 refresh，失败按 `auth` 冷却或禁用
- 余额不足映射 `quota`，默认长冷却
- 连续 5xx 阈值冷却
- 404 短冷却、不污染 auth

先不要做、除非单独开产品开关：

- 同名模型跨 provider route pool
- 每日 09/21 自动签到
- 按积分排序选号
- 容器重启才加载新账号
- 动态目录失败时的静态模型白名单

签到会改变上游配额，属于对 WorkBuddy 账号的主动行为。本仓库当前对 Qoder 没有等效动作。若以后做，也必须是账号级 opt-in，默认关，且失败不能让 chat 路径卡住。

## 详细实施计划

当前 **不开始写代码**。下面是排进里程碑后的顺序。依赖：H7 至少完成空安装登录聊天；Phase I 的 canonical conversation 可以并行设计，但 WorkBuddy 的 OpenAI 入口不需要等 Anthropic 做完。

### J0 — 控制面先认识 provider

目标：Qoder 行为零变化，数据库和 API 能记下类型。

- accounts 增加 `provider` 与 `provider_region`；历史空值按 `qoder/global` 读
- Create / Import / GET 列表读写 `provider` 与 `region`
- 增加静态 `ProviderRegistry` 与 `ProviderDescriptor`；非法 provider / region / credential format 直接 400
- 前端下拉值由 descriptor 生成并与后端枚举对齐
- Manager 通过 descriptor 判断 runtime；只有 `qoder + child_process` spawn daemon
- 回归：现有账号重启后仍是 Qoder，worker 数不变

完成标准：控制台创建 Qoder 账号与现在完全一致；用 API 写入未知 provider 被拒绝。

### J1 — Runtime / Executor 接口

目标：调度器看到的是账号，执行器按 kind 分发。

- 落地小接口：`CredentialCodec`、`LoginSessionProvider`、`ChatExecutor`、`ModelCatalogProvider`、`ErrorClassifier`
- `Pool.Item` 带 provider family、region、runtime kind 与 route capabilities
- `Pool.Pick` 支持 route-aware 查询：public model、provider family、账号 pin、excluded、capability
- `ChatExecutor` 按 item 分支；Qoder 分支保持现测例
- Models 拉取按账号分发，再在 API 层合并
- 内存 ModelRouteRegistry 从各账号目录构建 `public model -> provider -> native model -> account`
- 登录/导入/export 由 provider capability 接口路由，而不是一律 worker proxy
- 单测：假 in-process adapter 能被 pick 到，且不启动 Node

完成标准：

1. 不接真实 WorkBuddy 也能跑通「非 Qoder 账号不拉起 daemon」。
2. 请求不支持的模型时不会 pick 到不相关账号。
3. 尝试次数按当前 route pool 候选数计算，而不是整个 `Pool.Len()`。

### J2 — WorkBuddy adapter（可用的最小闭环）

目标：一个 CN 或 global 账号能从控制台登录并聊天。

- Go 包建议：`internal/providers/workbuddy`（auth、headers、payload、sse、client）
- 浏览器登录：state + poll + 写 SQLite
- 请求前 refresh；session dead 禁用
- chat stream / aggregate / tools / reasoning_content
- 动态模型目录，失败明确报错
- 错误映射进现有 taxonomy
- 控制台表单和登录提示由 WorkBuddy descriptor 生成；无 PAT tab，不写死 provider 分支
- 导入导出 `workbuddy-oauth-v1`
- 单测用 httptest 固定 state/token/chat/models，不打真上游

完成标准：

1. 空账号 → 选 WorkBuddy → 浏览器登录 → `只回复OK`
2. 导入 JSON → 无需浏览器即可 chat
3. 同池一个 Qoder + 一个 WorkBuddy：pin Qoder 仍走 daemon，pin WorkBuddy 不碰 Node
4. Qoder 原有 stream/tools/reasoning/usage 测试全绿

### J3 — 池质量（可选，但建议紧跟 J2）

- 余额探测写入账号视图，不改变默认 RR
- 402 / 积分不足 → quota 冷却
- 基线 failover 先限制在 **同 provider family** 内
- 健康：token 过期、refresh 失败、upstream 5xx
- 不默认自动签到

完成标准：WorkBuddy A 429 时，请求能落到 WorkBuddy B；`cross_provider_model_pool=false` 时不会误打到 Qoder。

### J4 - 同名模型 Route Pool 与协议

- 增加 `cross_provider_model_pool` 开关，默认关闭
- 开启后同名 bare ID 进入 route pool；provider 前缀 ID 强制单一 provider
- provider family round-robin + provider 内 account round-robin
- failover 只限同一个 public model ID 的 route pool
- capability filter 覆盖 context、tools、images、reasoning
- `model_settings` provider-scoped，避免同名模型共享冲突 context
- 响应 `model` 重写回 public model ID，并带实际 provider 观测信息
- Access 页测试聊天能选 WorkBuddy 账号
- Phase I canonical conversation 落地后，WorkBuddy adapter 消费 canonical，而不是让 `/v1/messages` 直接拼 WorkBuddy JSON
- OpenAI 路径可继续短接（上游已是 Chat Completions），但工具 ID 映射必须走同一套 canonical 规则

完成标准：

1. 一个 Qoder 账号 + 一个 WorkBuddy 账号都提供 `glm-5.2` 时，开启开关后两次请求分别落到两个 provider family。
2. Qoder A 429 时可切到 Qoder B；Qoder 全部不可用时可切到同 route pool 的 WorkBuddy。
3. `qoder/glm-5.2` 永远不会打到 WorkBuddy；账号 pin 优先且不支持时不静默换号。
4. 流式响应首字节前可 failover，首字节后不重放。
5. Qoder 原有模型列表、模型映射、stream / tools / reasoning / usage 测试全绿。

### J5 - 以后的上游

接口稳定后，新上游 = 新 provider descriptor + capability implementations + credential format，不再改 SQLite 主表。

候选顺序：

1. WorkBuddy（协议公开、纯 HTTP、和现产品最像）
2. Anthropic `/v1/messages` 入口（这是协议，不是账号类型）
3. Cursor（等 Qoder + 协议边界更稳；它的反检测/协议漂移成本高于 WorkBuddy）
4. 其他 CLI 登录态网关：先问 runtime kind，再问要不要 child process

Cursor 不能复用 WorkBuddy client，只能复用 J0/J1 的注册表和 executor 接口。

Qoder CN 不是第三种 runtime，也不是新 family。它复用现有 Qoder worker，按 `region` 换 CLI 包、HOME 目录名和 fallback host。设计与任务见下文「Qoder CN」。

## WorkBuddy 参考实现与代码使用边界

### 参考对象

WorkBuddy / CodeBuddy 协议实现参考：

- Repository: <https://github.com/Sliverkiss/workbuddy2api>
- Branch: `master`
- Commit: `92514d8ba06413c3b620e96da3ebc38e6c7beda0`
- Commit date: `2026-08-01`
- Scope: 只作为 WorkBuddy / CodeBuddy 协议事实来源，不作为本仓库架构蓝本

授权状态：该仓库 README 写了 `MIT`，但仓库中没有 `LICENSE` 文件，GitHub license API 返回 404。
因此在 license 状态明确前，**默认只参考行为和协议，不直接复制代码**；实现时按本仓库包结构重写。
如果作者补充正式 MIT LICENSE，或明确确认授权，再考虑在保留版权声明的前提下移植少量独立 helper。

### 可参考并重写的代码

| 参考文件 / 位置 | 可吸收的内容 | 本仓库落点 |
|-----------------|--------------|------------|
| `internal/auth/auth.go` | 嵌套 / 扁平 auth JSON 兼容、domain -> region、原子写回思想 | `internal/providers/workbuddy/credential.go` |
| `internal/upstream/headers.go` | common / chat / billing / refresh headers 分离；refresh token 不进 chat | `internal/providers/workbuddy/headers.go` |
| `internal/upstream/payload.go` | 强制 `stream=true`、`tool_choice` 字符串化 | `internal/providers/workbuddy/payload.go` |
| `internal/upstream/sse.go` | SSE 聚合、按 index 合并 `tool_calls`、保留 `reasoning_content` | `internal/providers/workbuddy/sse.go` |
| `internal/upstream/client.go` | refresh、models、chat、billing endpoint 形态与错误分类标记 | `internal/providers/workbuddy/client.go` |
| `internal/upstream/*_test.go` | httptest 契约用例、错误响应 fixture 思路 | 本仓库 provider 契约测试 |
| `cmd/login/main.go` | state + poll 登录流程、独立 cookie jar | `LoginSessionProvider`，不保留 CLI 工具形态 |

“可参考并重写”不是 copy-paste 许可。命名、错误类型、存储、并发控制、测试断言都必须按本仓库重写。

### 不使用或不照搬的代码

| 参考位置 | 不用的原因 |
|----------|------------|
| `internal/pool/pool.go` | 文件账号池、按积分选号，与 SQLite + 单一 Go 调度器冲突 |
| `internal/scheduler/scheduler.go` | 09/21 签到、22 点 keepalive 是 WorkBuddy 产品行为，不是通用 provider 契约 |
| `internal/server/handler.go` | 第二套 OpenAI server；本仓库已有 ingress / auth / console |
| `cmd/server` | 独立进程与配置模型，不符合单容器控制面 |
| `login.sh` / `credit.sh` / `signin.sh` | CLI 与脚本运维形态；本仓库走控制台和 API |
| `config.example.json` / `docker-compose.yml` | 不复制部署假设 |
| 静态模型 fallback 表 | 模型目录失败必须显式报错 |

### 引用原则

- 协议事实可以参考：endpoint、header、payload 约束、SSE 形态、错误标记
- 架构不参考：文件账号池、积分选号、独立 server、脚本登录、静态模型表
- 代码默认重写；license 明确后再评估逐段移植
- 不把参考仓库的 CLI UA、CodeBuddy header、协议常量放进 Qoder adapter
- 不引入参考仓库内出现的私有路径或内部项目名

## 风险

- **服务条款**：与 Qoder 相同，只允许用户自己有权使用的账号。控制台文案保持克制，不承诺「无限额度」。
- **协议漂移**：WorkBuddy 用伪造 CLI UA 和未文档化的 `/v2/plugin/*`。必须把 host、path、头做成适配器常量并配契约测试；上游一变只改这一个包。
- **模型撞名**：默认模式下 bare ID 继续留给 Qoder，WorkBuddy 用 provider 前缀；开启 Route Pool 后必须先通过 capability filter 与 deterministic 调度，不能随机落到错误上游。
- **跨 provider failover**：Qoder quota 不等于 WorkBuddy 积分。基线只同 provider family failover；同名 Route Pool 必须显式开启，且只允许在同一 public model ID 内切换。
- **能力不一致**：同名模型的 context、tools、images、reasoning 可能不同。请求前必须过滤 route target，不能假设两个 provider 的同名模型完全等价。
- **安全**：token 只进 SQLite payload，权限 0600；`/api/accounts` 不回凭据；export 需 API key。
- **范围膨胀**：签到、积分看板、keepalive 很容易把 Accounts 做成第二个 workbuddy2api。第一期只做登录和聊天。
- **H7 / I 未完成**：现在加 WorkBuddy 会冻住 Qoder 控制面。先把 Qoder 账号路径收口。

## 对本仓库可用性的判断

| 能力 | 现在 | J0–J1 之后 | J2 之后 |
|------|------|------------|---------|
| 多 Qoder 账号 | 可用 | 可用 | 可用 |
| 控制台选非 Qoder 类型 | 看起来可以，实际丢字段 | 可持久化 | 可用 |
| WorkBuddy 聊天 | 不可用 | 不可用 | 可用 |
| 混合池 | 不可用 | 结构允许 | 可用；默认同 provider family 切流量 |
| 同名模型跨 provider pool | 不可用 | 结构允许 | J4 后显式开启可用 |
| 再加 Cursor | 要挖控制面 | 主要写 adapter | 主要写 adapter |
| Anthropic 入口 | 未做 | 未做 | 仍按 Phase I，与 WorkBuddy 正交 |

所以：仓库 **作为 Qoder 网关是完整的**；**作为多 CLI 网关还差一层 provider 抽象**。这层并不大，但必须先做，否则 WorkBuddy 会以「再开一个 executor 特例」的方式污染 `manager.go` / `chat.go`。

## 建议的产品决策（实现前锁定）

1. WorkBuddy 是账号类型，不是新的监听端口或新容器。
2. 默认调度仍是 round-robin + pin；积分只展示、只作为 quota 信号。
3. 自动签到默认关闭。
4. 动态模型失败不回退静态表。
5. 同名模型不静默跨上游；跨 provider Route Pool 必须显式开启，并只限同一个 public model ID。
6. WorkBuddy 不启动 Node。
7. 当前里程碑仍是 Qoder；本文不是开工许可。

排期建议：H7 验收通过后再开 J0。J0/J1 可以在 Phase I 之前做，因为它们不碰 Anthropic。J2 需要真实账号和契约测试。Qoder CN 不依赖 Phase I，可在 WorkBuddy 代码稳定后作为独立 Qoder 里程碑开工。

---

# Qoder CN（中国大陆版）

last-updated: 2026-08-28
status: 代码完成（pin 1.1.32）；L6 真实账号验收未完成
cli-pin: `@qoder-ai/qodercli@1.1.32` 与 `@qodercn-ai/qoderclicn@1.1.32`
reference-proxy: [lininn/qorder-proxy](https://github.com/lininn/qorder-proxy) `main` @ 2026-08-26（只借包名 / 登录入口，不借进程模型）

## 结论

**和 qodercli 几乎是同一份程序，不是新协议。** 不能当成 WorkBuddy 那种 in-process HTTP adapter。也不要做成新的 `qodercn` provider family。

| 问题 | 答案 |
|------|------|
| 是不是只换 API endpoint？ | 对执行路径几乎是。chat 仍走 WASM `prepareInferRequest` → 上游 SSE。host 由 CLI 的编译期站点开关决定，不是 Go 拼 URL。 |
| 和 qodercli 差多少？ | 1.1.27 的 minify bundle 只差约 10 字节量级的站点常量和加密种子。worker 现有 5 条 needles **全部命中** CN bundle。 |
| 能不能共用一个 daemon 文件？ | 能。继续 `worker/src/daemon.mjs`。按账号 region 换 `QODERCLI_JS`、config dir、fallback host。 |
| 能不能共用一个 Node 进程？ | 不能。WASM / AuthManager 仍然进程全局；CN 与 Global 必须各起一个 child。 |
| SQLite 要不要新表？ | 不要。`provider` / `provider_region` 已在。缺的是 descriptor 允许 `qoder + cn`，以及 spawn 链认识 region。 |
| 要不要新 family `qodercn`？ | 不要。`provider=qoder`，`region=cn`。控制台显示「Qoder 国内版」。 |
| 能不能抄 qorder-proxy？ | 不能抄架构。它每请求 spawn 一次 CLI，违反本仓库硬规则。包名、PAT 入口、双后端对照可以参考。 |

一句话：Qoder CN 是 **同一条 Qoder 执行路径上的第二套 pinned CLI**。spawn 链已按 region 分支；剩下的是 L6 真实账号验收。

## 调研事实（1.1.27）

npm 上的中国版包名是 `@qodercn-ai/qoderclicn`（bin `qoderclicn`，bundle `bundle/qoderclicn.js`），不是 `qodercncli`。国际版是 `@qoder-ai/qodercli`（bin `qodercli`，bundle `bundle/qodercli.js`）。两边按同一版本号同构发布；2026-08-28 起本仓库钉 **1.1.32**（此前为 1.1.27，升级时重新验证了全部 5 条 needles）。

同一份源码，编译期写入站点：

```js
// global: Pa=Xi="cn"==(IAs="global")  → Xi=false
// cn:     Pa=Xi="cn"==(IAs="cn")      → Xi=true
```

`QODERCLI_SITE` 环境变量只影响 `QODERCN_CONFIG_DIR_NAME` / `QODER_CONFIG_DIR_NAME` 这类辅助函数，**不能**把国际版二进制翻成国内版。必须装第二份包。

`Xi=true` 时的有效差异：

| 项 | Global (`Xi=false`) | CN (`Xi=true`) |
|----|---------------------|----------------|
| npm | `@qoder-ai/qodercli` | `@qodercn-ai/qoderclicn` |
| bundle | `qodercli.js` | `qoderclicn.js` |
| 配置目录名 | `.qoder` | `.qoder-cn` |
| 环境前缀 | `QODER_` | `QODERCN_` |
| 配置目录 env（CLI 真的会读） | `QODER_CONFIG_DIR` | `QODERCN_CONFIG_DIR` |
| PAT 环境变量（CLI 自己读） | `QODER_PERSONAL_ACCESS_TOKEN` | `QODERCN_PERSONAL_ACCESS_TOKEN` |
| chat / gateway | `api1.qoder.sh` / `api2` / `api3` | `gateway.qoder.com.cn` |
| openapi / quota | `openapi.qoder.sh` | `openapi.qoder.com.cn` |
| 产品域名 | `qoder.sh` | `qoder.com.cn` |
| 登录 API | `loginWithDeviceFlow` + `loginWithPAT` | 同样存在 |
| 凭证落盘 | `{configDir}/.auth/user` + `machine_id` | 同样结构，换目录 |
| WASM needles | 本仓库 5 条全中 | 同样全中 |

本仓库 worker 真正 POST 的 URL 仍来自 WASM `encoded.url`。Go / daemon **不要**手写 `/algo/api/v2/service/pro/sse/agent_chat_generation`。只有 `hotEndpoint` 还没从 `prepareInferRequest` 拿到时，才允许按 region 填 fallback：

- global: `https://api1.qoder.sh`
- cn: `https://gateway.qoder.com.cn`

凭证格式保持 `qoder-native-v1`。CN blob 与 Global blob 不能混用；region 在账号行上，不进 blob 本身。

## 和 qorder-proxy 的边界

[lininn/qorder-proxy](https://github.com/lininn/qorder-proxy) 把 CN / Global 做成两个 spawn-CLI 后端。

可参考：

- 包名 `@qodercn-ai/qoderclicn` / `@qoder-ai/qodercli`
- CN PAT 创建页 `https://qoder.com.cn/account/integrations`
- 产品文案上的「两个站点，两套 CLI」

不要用：

| 参考做法 | 原因 |
|----------|------|
| 每请求 `qoderclicn --print --output-format json` | `AGENTS.md` 禁止 spawn 完整 CLI；本仓库要热 `QoderContext` |
| 认证目录 `~/.qoderworkcn`、`.auth-cn/user` | 与 1.1.27 实际目录 `.qoder-cn/.auth` 不符，多半过时 |
| 静态模型表 `qoder-cn` / `auto` / 手写 Qwen 列表 | 本仓库 catalog 失败就报错，不猜 |
| 用 prompt 解析 tool call | 本仓库已有 WASM + nested SSE 工具路径 |
| 进程级 `CLI_BACKEND=cn\|global` 单后端 | 本仓库要同一控制面里同时跑 Global 账号和 CN 账号 |

## 当前仓库卡住的点

J0 已经让 SQLite 认识 `provider` / `region`，但 Qoder descriptor 只注册了 `global`。`TestStoreRejectsUnknownProviderAndRegion` 明确要求 `qoder + cn` 返回错误。

spawn 链完全不知道 region：

| 位置 | 现状 |
|------|------|
| `internal/providers/registry.go` | Qoder 只有 `global`；注释写「region 只是标签」 |
| `ExecStarter` | 永远 `HOME={runtime}`、`QODER_HOME={home}/.qoder`（CLI **不读** `QODER_HOME`）、`QODERCLI_JS=qodercli.js` |
| `materializeHome` / `SyncCredential` | 永远读写 `.qoder/.auth` |
| `worker/src/rewrite-loader.mjs` | 只 hook 文件名含 `qodercli.js` 的模块 |
| `worker/src/daemon.mjs` | fallback `https://api1.qoder.sh`；默认路径只有国际版包 |
| `Pool.PickRoute` | 只过滤 provider family，不过滤 region |
| `resolveProviderFilter` | bare ID 固定 `qoder`，CN 与 Global 会进同一候选池 |
| 前端 `labelKeys` | 没有 `qoder-cn`；`accountProviderLabel` 把任意 qoder 都显示成国际版 |

WorkBuddy 的 region 只换 Go 里的 host 常量。Qoder CN 必须换 **CLI 二进制 + HOME 目录名**，这是唯一多出来的工作。

## 目标形态

```text
Client
  -> Go :3010
    -> accounts.Pool（provider=qoder 时默认同 region failover）
      -> region=global: Node daemon + @qoder-ai/qodercli@1.1.32
           HOME/.qoder/.auth  -> api*.qoder.sh
      -> region=cn:     Node daemon + @qodercn-ai/qoderclicn@1.1.32
           HOME/.qoder-cn/.auth -> gateway.qoder.com.cn
```

规则：

- 不新增 provider family，不新增 SQLite 列，不新增 worker 入口文件
- 不把 CN 做成 in-process HTTP
- 升级 pinned CLI 版本时两边一起升，且先在两边的新 bundle 上重新验证全部 5 条 needles（2026-08-28 已从 1.1.27 一起升到 1.1.32，`worker/src/compat.mjs`）
- 登录仍走现有 worker `/admin/login/{device,status,pat}`；`account.Provider == "qoder"` 的分支保持
- 导入导出仍是 `qoder-native-v1`；创建 / import 必须带对的 `region`。export JSON 要带 `provider` 与 `region`，否则 CN blob 会被默认成 Global
- 公共模型前缀仍是 `qoder/`。不要引入 `qodercn/`，否则会把 region 做成第二套 family
- 默认同 region failover。Global 429 不得落到 CN，反过来也不行。账号 pin 除外
- `CROSS_PROVIDER_MODEL_POOL` 仍只影响跨 family（Qoder vs WorkBuddy），不管 CN vs Global

## 产品决策（实现前锁定）

1. 控制台选项是 `Qoder Global` 与 `Qoder 国内版`，提交 `provider=qoder` + `region=global|cn`。
2. CN 支持浏览器 device-flow、PAT、native import；CLI 源码里两种登录都在。若真实 CN device-flow 不可用，再降级为 PAT + import，不要一开始就砍掉浏览器。
3. CN PAT 向导文案指向 `https://qoder.com.cn/account/integrations`；不要把国际版 PAT 填进 CN 账号。
4. 调度按 **provider + region** 隔离 failover。不要按积分、站点亲和或模型名猜。
5. 镜像同时安装两套 1.1.32 CLI。体积换正确性。
6. 不把 `QODERCLI_SITE=cn` 当作「一个二进制两种站点」的方案。

## 实施顺序

当前 **不开始写代码**。清单进 `docs/PLAN.md` Phase L。依赖：现有 Qoder worker 路径保持绿灯。不依赖 Anthropic / Cursor。

### L0 — descriptor 承认 `qoder + cn`

- Qoder `Regions` 增加 `cn`：`ChatBase=https://gateway.qoder.com.cn`，`AuthBase=https://qoder.com.cn`，`DefaultDomain=qoder.com.cn`
- 历史空 region 仍读 `global`
- `TestStoreRejectsUnknownProviderAndRegion` 改为接受 `qoder+cn`，继续拒绝未知 family
- 前端此时还不必展示；先让 API 能建出来
- **不要单独合 L0**：`enabled=true` 的 CN 账号在 L2 之前会按国际版 CLI + `.qoder` spawn。L0–L2 同一变更集，或 L0 只允许 CN 账号 `enabled=false`

完成标准：`POST /api/accounts {provider:qoder, region:cn}` 不再 400；现有 Global 账号重启行为不变。

### L1 — worker 按 CLI 文件名工作，而不是写死国际版

- `rewrite-loader.mjs` hook `qodercli.js` **或** `qoderclicn.js`
- `compat.mjs` 钉 1.1.32；错误信息同时提两个包名
- `daemon.mjs` 默认路径同时找两个 bundle；fallback host 按实际加载的文件名或 worker-only `QODER_SITE=cn|global`（不要用 `QODERCLI_SITE`，它翻不了编译期 `Xi`）
- 配置目录由 Go 显式传入 CLI 能读的变量：Global `QODER_CONFIG_DIR={home}/.qoder`，CN `QODERCN_CONFIG_DIR={home}/.qoder-cn`。现有 `QODER_HOME` 是本仓库自己的标签，**1.1.32 CLI 不读它**；Global 今天能工作是因为 `HOME={runtime}` + 默认 `~/.qoder`

完成标准：用 `QODERCLI_JS=.../qoderclicn.js` 启动 daemon 时 needles 通过，不再因为文件名不是 `qodercli.js` 而跳过 patch。

### L2 — Manager 按 region spawn

- `ManagerConfig` 增加 `QoderCNCLIPath`（env `QODERCNCLI_JS`）
- `ExecStarter` 按 region 分支：
  - 共用：`HOME={runtime}`、`QODER_SITE`、`QODERCLI_JS`
  - global：`QODERCLI_JS=QoderCLIPath`，`QODER_CONFIG_DIR={home}/.qoder`
  - cn：`QODERCLI_JS=QoderCNCLIPath`，`QODERCN_CONFIG_DIR={home}/.qoder-cn`；缺路径则 spawn 失败，不要静默回落到国际版
- `materializeHome` / `SyncCredential` 必须吃到 `account.ProviderRegion`，读写 `.qoder` 或 `.qoder-cn`。今天这两个函数只拿 `accountID`，签名要改
- export `/api/accounts/:id/export` 补 `provider` + `region`；import 缺 region 时仍默认 global（兼容旧包），有 region 则尊重
- 非法组合：Qoder + 未知 region 仍 400；WorkBuddy 路径一行不动

完成标准：启用一个 CN 账号只拉起一个 Node，且 `QODERCLI_JS` 指向 `qoderclicn.js`；启用一个 Global 账号仍指向 `qodercli.js`。

### L3 — 同 region failover

- `RouteQuery` 增加 `RegionFilter`；空 region 按 `global` 读，兼容旧 `Pool.Item`
- **改的是 `executor/chat.go` 的 failover 循环，不只是 pool**：`pick` / `attemptsFor` / `ChatNonStream` / `ChatStream` 都只传了 `providerFilter`。第一次选中后把 `item.Region` 钉进后续 Pick，attempts 按 provider+region 计数
- bare `glm-5.2` 仍只过滤 `provider=qoder`；未 pin 时用池里第一个可用 Qoder 账号的 region，不要在同一请求里跨 CN/Global
- 账号 pin 仍优先。sticky-escape（pin 账号冷却）只逃到 **同一 region**，不再逃到整个 qoder family
- 单测：Global A 429 可切 Global B，不可切 CN；pin CN 且 CN 冷却时，不得落到 Global
- L3 必须在 L4 控制台放出 CN 之前合并，否则混合池会把 Global 限流打到 CN

完成标准：混合池里 pin / 限流行为可测，且不改 WorkBuddy 的 family 过滤。

### L4 — 控制台

- `AddAccountModal` 增加 `qoder-cn` i18n key；不要再把无 key 的 region 丢掉
- `accountProviderLabel` 按 `qoder + cn` 显示国内版
- PAT 辅助说明按 region 切换（CN 指向 qoder.com.cn integrations）
- 账号卡片继续用 Qoder mark；用 region chip 区分，不加第二套品牌色

完成标准：Accounts 能选出 Qoder 国内版，创建后列表不再写成「Qoder 国际版」。

### L5 — 镜像与配置

- `deploy/Dockerfile` 同时 `npm i -g @qoder-ai/qodercli@1.1.32 @qodercn-ai/qoderclicn@1.1.32`
- 默认 env：`QODERCLI_JS=.../qodercli.js`，`QODERCNCLI_JS=.../qoderclicn.js`
- `internal/config` 读取第二条路径
- 文档：README 账号类型多一行国内版；CHANGELOG Unreleased 双语

完成标准：单容器镜像里两个 bundle 都在，缺 CN 包时 CN 账号启动要响亮失败。

### L6 — 验收

1. 空 CN 账号 → 浏览器或 PAT 登录 → `只回复OK`
2. 导入 CN `qoder-native-v1` → daemon hot，不经浏览器
3. 同池 Global + CN：pin 各自走对应 CLI；Global 429 不打到 CN
4. 原有 Qoder stream / tools / reasoning / usage / quota 测试全绿
5. WorkBuddy 回归不受影响

## 风险

- **站点开关是编译期的**：装错包或只改 env，账号会打到错误云。启动时在 daemon 日志里打出 `site=cn|global` 和 bundle 路径。
- **HOME 目录名**：写到 `.qoder` 的 CN 凭证会被国际版 CLI 当成自己的，表现为怪认证错误。materialize / sync 必须按 region 分支，并覆盖回归测试。
- **needles 漂移**：两边同构发布（1.1.32 起继续如此）；以后升 CN 而不升 Global（或反过来）会让一边 hooks 失效。两边继续锁同一版本，升级时一起升并重新验证 `compat.mjs` 的 5 条 needles。
- **镜像体积**：两份 ~28MB CLI。可接受。不要试图用一份 JS 加 env 伪造站点。
- **device-flow 是否真能在 CN 用**：源码有函数不等于生产授权页可用。L6 用真实号验证；失败则控制台把 CN 的浏览器 tab 降为次要，PAT 为主。
- **服务条款**：只允许用户自己的国内版账号。文案不承诺「国内无限额度」或跨站共用。
