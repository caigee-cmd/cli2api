---
id: cli2api-behavior-preserving-refactoring
title: 后端工程化重构实施手册（行为保持）
scope: [backend, package-boundaries, refactoring, compatibility, testing, rollout]
status: in-progress
read-when: 评估或执行不改变现有功能的后端职责拆分、制定重构 PR、检查兼容性与回滚条件时
summary: 基于现有实现的渐进式工程化重构方案，包含目标职责、迁移映射、状态所有权、16 个实施阶段、测试矩阵、PR 规则、发布回滚和完成标准。S00–S03 已验收；跨包迁移尚未开始。
related: [AGENTS.md, docs/ARCHITECTURE.md, docs/REQUEST.md, docs/PLAN.md, docs/DEVELOPMENT.md]
last-updated: 2026-09-18
---

# 后端工程化重构实施手册

> **目标：外部契约不变，内部职责归位。以后加功能时，能明确知道改哪里、影响哪里、怎么验证。**
>
> 这是实施方案，不是已完成报告。所有待办初始均未完成。编写本文时只阅读代码，没有执行重构、运行功能测试或验证真实账号。
>
> 编写本文时的代码观察点为 `9c01479`。S00 已在 `624874a`（当时最新 `origin/main`，含 #189）重新核对；后续阶段仍须同步最新 main，不能将任一提交当成永远固定的实现事实。

## 文档定位与使用约定

1. 本文因用户明确要求独立实施文档而新增，是现有“不新增额外计划文件”规则的一次明确例外，不代表可以继续增加散落的 TODO、NOTES 或方案文件。
2. `AGENTS.md` 仍是硬规则入口；`ARCHITECTURE.md` 与 `REQUEST.md` 仍是现行行为契约；`PLAN.md` 仍管理当前里程碑和优先级。
3. 本文描述的是**拟议目标结构**。在跨包迁移获准、相应规则更新前，不能声称现有 `auth / endpoint / executor / translate / api` 边界已经失效。
4. 文中出现的新增文件、接口名和测试名是拟议位置，不是声称它们已经存在。不照目录机械创建空文件。
5. 不因本文扩大 provider 里程碑：只整理当前已经实现的 Qoder、WorkBuddy、Trae、Devin。Messages 是已有入口协议，不等于开始接入 Anthropic provider。
6. 本文的任务复选框与阶段验收记录是本轮重构详细进度的唯一来源，执行者必须随任务更新；`PLAN.md` 只保留里程碑摘要、已验收阶段和本文链接，不复制逐项清单。阶段验收后同步摘要，避免两份记录相互矛盾。
7. 本文不授权部署、真实账号调用、重启服务或合并 PR。涉及外部副作用时另行确认环境和权限。

---

## 目录

1. [目标、范围与红线](#1-目标范围与红线)
2. [现状与真正要解决的问题](#2-现状与真正要解决的问题)
3. [目标结构与依赖方向](#3-目标结构与依赖方向)
4. [文件迁移地图](#4-文件迁移地图)
5. [状态、类型与接口归属](#5-状态类型与接口归属)
6. [必须保持的行为契约](#6-必须保持的行为契约)
7. [实施阶段总览](#7-实施阶段总览)
8. [阶段 S00—S15：逐步实施](#8-阶段-s00s15逐步实施)
9. [测试与差分验证方案](#9-测试与差分验证方案)
10. [PR、评审与持续集成](#10-pr评审与持续集成)
11. [发布、观测与回滚](#11-发布观测与回滚)
12. [风险与暂停条件](#12-风险与暂停条件)
13. [完成定义与后续功能定位](#13-完成定义与后续功能定位)
14. [执行记录模板](#14-执行记录模板)

---

## 1. 目标、范围与红线

### 1.1 本轮必须达成

- 拆开 HTTP、应用编排、运行时管理、调度、provider 协议和 SQLite 实现。
- 消除“新增功能默认塞进 Server 或 Manager”的惯性。
- 保持唯一执行路径，公有协议与控制台 chat 共用执行能力。
- 保持现有数据库、凭据、运行时资产、API 和前端兼容。
- 将关键边界变成测试与 import 约束，而不只依赖口头约定。
- 每个合入节点可构建、可验证、可回滚。

### 1.2 本轮明确不做

- 不增加 provider、模型能力、协议功能、路由策略或配置项。
- 不更换 HTTP 路由器、SQLite 驱动、日志系统、组件库或依赖注入框架。
- 不引入 Redis、事件总线、插件平台、通用 Repository 框架。
- 不重写 SSE 编解码、错误分类、调度算法、登录流程。
- 不优化缓存 TTL、超时、重试间隔、锁粒度、刷新频率和启动并发度。
- 不调整前端 IA、API 路径、安装流程、部署目录与 worker 协议。
- 不把 `translate/executor/logs` 同时改名成 `conversation/scheduler/observe`。
- 不以重构为理由清理历史数据、变更 schema 或重写历史 migration。

### 1.3 如何理解“不改变行为”

不仅是“正常请求还能成功”，还包括：

- 相同输入得到相同类别的状态码、错误体、响应字段和事件顺序。
- 同时存在多个错误时，校验先后顺序与原实现一致。
- 请求取消、流中断、进程故障和更新期间的处理方式不变。
- 相同账号状态下的选号、重试、冷却和会话绑定语义不变。
- 账号操作失败时，已经发生的写库和进程副作用不被悄悄改变。
- 缓存和持久化状态的所有权、刷新时机与可见性不变。
- 初始化、后台任务启动、日志完成、关闭资源的先后关系不变。

不能数学上保证任意环境和所有未来功能永远兼容。工程上的保证方式是：明确兼容面、补齐保护测试、缩小变更、真实验收、可回滚。

### 1.4 遇到现有 bug 或安全问题

- 记录并单独处理，不把修复藏在移动代码的 diff 中。
- 如果问题使测试基线不可靠或会造成安全风险，暂停相关阶段。
- 必要修复独立 PR，重新建立基线后继续。
- 例如发现敏感信息日志，不应将其复制进文档、fixture 或 PR 描述；整改单独评估。
- 不以“保持行为”为理由保留未经授权的访问或执行危险的测试。

---

## 2. 现状与真正要解决的问题

### 2.1 已观察到的职责混合

| 位置 | 当前包含的职责 | 拆分方向 |
|---|---|---|
| `internal/api/server.go` | 初始化数据库、设置、日志、manager、provider、executor；路由、鉴权、overview、health | app / server / console |
| `internal/api/chat.go` | OpenAI HTTP、SSE、模型目录与缓存、请求准备、日志收尾 | gateway / executor / 共享模型服务 / logs |
| `internal/api/compat.go` | Messages、Responses、流转换、执行调用 | gateway，保留 translate 转换边界 |
| `internal/api/workerproxy.go` | Qoder worker 通信、登录及相关 HTTP 处理 | provider 具体协议与 console HTTP 分离 |
| `internal/accounts/manager.go` | CRUD/import/view、进程启停、恢复、刷新、签到保活 | control / runtime / providers/qoder |
| `internal/accounts/pool.go` | 调度候选、权重、路由、冷却、状态快照 | executor 的调度职责 |
| `internal/accounts/classify.go` | 错误归类、Retry-After、冷却与退避辅助 | executor 策略；provider 语义保持中立 |
| `internal/accounts/store.go` 等 | 实体、SQLite、设置、凭据等 | accounts 轻量类型 + store 实现 |
| `internal/logs/recorder.go` | 请求记录和持久化关联 | 保留 logs 名称，断开对具体 SQLite 的依赖 |
| `internal/providers/workbuddy/client.go` 等 | 已有 provider 实现和消费方 Store 接口 | 复用已有接口，不再造一套同类抽象 |

文件行数只能提示复杂度，不作为验收指标。把一个 1,000 行文件分成十个文件，不代表依赖变得更清晰。

### 2.2 现有能力不能因目录整齐而丢失

当前入口除三种公有聊天协议外，还包括：

- `/v1/models`、`/api/models` 及其设置子路径；
- `/api/providers`；
- overview 与 summary；
- 账号集合、导入与账号子路径下的操作；
- client keys 与 console key；
- logs 查询、详情、统计等子路径；
- system settings 与完整托管更新流程；
- webui 静态资源及限定的 SPA fallback 页面。

以正式实施时的 `routes()`、handler 内部路径分派和测试为准。顶层注册表不能替代对 `/api/accounts/`、`/api/logs/` 等内部 action 的盘点。

---

## 3. 目标结构与依赖方向

### 3.1 本轮目标目录

```text
cmd/
  server/                         配置入口、信号、HTTP listen/shutdown
  updater/                        不动

internal/
  app/
    app.go                        创建依赖、初始化、启动、关闭
    providers.go                  注册当前已有 provider

  server/
    router.go                     原路由注册和 webui fallback
    middleware.go                 鉴权包装、CORS、maintenance
    health.go                     原 health 契约

  api/                            迁移期旧入口；不追加新业务

  gateway/                        第一轮一个 Go package
    handler.go
    openai.go
    openai_stream.go
    anthropic.go
    anthropic_stream.go
    responses.go
    responses_stream.go
    models.go
    errors.go

  console/                        第一轮一个 Go package
    handler.go
    overview.go
    accounts.go
    keys.go
    models.go
    providers.go
    logs.go
    system.go
    update.go
    chat.go

  executor/
    chat.go                       执行与 failover
    prepare.go                    共享请求准备
    session_affinity.go
    pool.go
    route.go
    classify.go

  translate/                      保留名字与现有语义，按需同包拆文件
  accounts/                       账号实体及必要的无副作用类型

  control/
    accounts.go                   账号操作编排和视图组合
    keys.go
    settings.go
    models.go                     共享目录读取/缓存、模型设置服务
    backup.go                     备份操作编排，不是 SQLite 备份 SQL

  runtime/
    manager.go                    生命周期、恢复、任务所有权
    child.go                      通用 child 跟踪，不写 Qoder CLI 参数
    probe.go                      探测/目录/配额刷新调度
    maintain.go                   签到保活的现有调度

  providers/
    interfaces.go
    registry.go
    registry_runtime.go           已有静态描述/运行时注册分工可保留
    reasoning.go
    routes.go
    qoder/                        Qoder HOME/daemon/登录/worker 具体协议
    workbuddy/
    trae/
    devin/

  store/
    sqlite.go
    migrations.go                 原 migration 内容原样保留
    accounts.go
    credentials.go
    api_keys.go
    secrets.go
    request_logs.go
    cooldowns.go
    checkin.go
    model_settings.go
    backup.go                     SQLite 备份、校验和保留策略的既有实现

  logs/                           recorder、ring、统计服务
  auth/
  endpoint/
  proxy/
  config/
  webui/
  update/                         checker、agent client、控制面更新协调
  updater/                        托管 agent 实现，不改行为
  buildinfo/

worker/                           pinned CLI + daemon，运行协议不变
frontend/                         页面、组件、请求路径、构建资产不变
```

说明：

- `store/checkin.go` 等是内容归属，不要求将某张表的少量代码立即单独成文件。
- `backup` 分两层：control 协调谁在何时请求备份；store 负责 SQLite 备份实现。不要复制实现。
- 更新任务状态机若需要离开 `api/update.go`，放入现有 `update` 控制面能力，不放进普通账号 control，也不侵入 `updater` agent。
- 若 executor 的调度部分后续确有独立边界，再开单独提案提取 scheduler。本轮不把整个 executor 更名。
- runtime 包可在调用处使用 `accountruntime` 别名，避免与 Go 标准库 `runtime` 混淆，不因此再造新层。

### 3.2 调用关系和 import 关系要区分

运行时调用方向：

```text
cmd/server → app（组装）
                 ├─ server → 已构建的 gateway / console handler
                 ├─ gateway ─┐
                 ├─ console ─┴→ executor → provider 能力
                 │       └────→ control → 存储/运行时窄接口
                 ├─ runtime → provider 能力、调度状态接口、持久化接口
                 ├─ store
                 └─ logs / update
```

上图中“调用持久化接口”不意味着 import `store`。由消费方定义小接口，app 注入 store 或装配适配器。

### 3.3 目标 import 约束

| 模块 | 可以依赖 | 禁止依赖 |
|---|---|---|
| app | 所有需要装配的模块 | 不应被任何底层模块引用 |
| server | HTTP、auth、endpoint、webui、handler 契约 | SQLite、provider 具体实现、运行时业务实现 |
| gateway | translate、executor、auth、provider 中立类型、消费方接口 | store、runtime manager、具体 provider |
| console | control、执行接口、logs/update 契约、auth | SQL/数据库驱动、具体进程启动实现 |
| executor | translate、providers 契约、auth、logs 类型、轻量账号类型 | app、server、gateway、console、store、具体 provider |
| control | 轻量类型、消费方接口、provider 契约 | HTTP handler、具体 SQL、具体子进程协议 |
| runtime | provider 契约、轻量类型、消费方接口 | gateway、console、control 服务、具体 provider 实现 |
| providers/<name> | providers 契约、translate、轻量账号类型、proxy、消费方接口 | executor、runtime manager、HTTP handler、store |
| store | SQLite、轻量数据契约、必要的纯函数 | app、HTTP handler、control、runtime manager、provider 具体实现 |
| logs | 日志数据、消费方持久化接口 | 具体 store、gateway、console |

不是要求一夜之间满足全部约束：先为历史边界设置临时例外，随后只减不增。禁止用接口中的 `any`、全局变量或 service locator 绕开依赖检查。

---

## 4. 文件迁移地图

| 当前来源 | 目标职责 | 迁移注意点 |
|---|---|---|
| `api.New()` | app | 初始化顺序、配置优先级和失败方式先原样保留 |
| `api.Server.Handler()/routes()` | server | ServeMux 匹配、OPTIONS、maintenance、静态 fallback 不变 |
| API/console key 包装 | server + auth | server 包装 HTTP，auth 解释身份和权限 |
| overview handlers | console | 数据读取走共享服务，保留 refresh 查询语义 |
| `handleChatCompletions` | gateway；console 使用同一执行路径 | 初期可给两条路由绑定同一个已构建 handler，避免复制流程 |
| `prepareChatExecution` | HTTP 输入提取 + executor.prepare | 不让执行层接收完整 `*http.Request` |
| `relayOpenAIStream` | gateway | Scanner 限制、flush、异常和 usage 收尾不变 |
| `compat.go` | gateway | Messages/Responses 分文件，现有 translate 行为不变 |
| 模型聚合、缓存、默认设置 | 共享模型服务 | `/v1/models`、`/api/models`、请求准备使用一致状态 |
| `api/accounts.go` | console + control | HTTP 解码/错误映射与副作用编排分离 |
| `api/workerproxy.go` | console + providers/qoder | 不能整文件搬到 provider 后让 provider 持有 HTTP handler |
| `api/keys.go` | console + control/auth + store | 权限语义、密钥生成与数据库实现分清 |
| `api/system_settings.go` | console + control | 保留落库与 live 值更新顺序 |
| `api/update.go` | console HTTP + update 协调 | maintenance 与 job 状态仍只有一个所有者 |
| `accounts.Manager` CRUD/import/view | control | 对 runtime 的调用先用桥接接口保持顺序 |
| `accounts.Manager` 启停/恢复/刷新 | runtime | 保留任务、锁、进程表和关闭所有权 |
| `ExecStarter` 等 Qoder 实现 | providers/qoder | runtime 持有通用 starter，不理解 HOME/CLI 参数 |
| `accounts.Pool/RouteQuery` | executor 调度职责 | 原算法、锁、快照版本、nil/empty 语义不变 |
| `accounts/classify.go` | executor 策略 | provider 分类结果不能反向引用 executor |
| `accounts.Store` 及相关方法 | store | 先断开类型循环，再整体迁移方法归属 |
| `accounts/grants.go` | auth 的权限语义 | 注意 auth 当前已依赖账号/key 类型，不能制造双向引用 |
| `accounts/request_logs.go` | logs 类型 + store SQL | SQL 聚合仍在 store；logs 通过接口调用 |
| `logs/recorder.go` | logs | 保留 recorder 行为，仅注入持久化接口 |

---

## 5. 状态、类型与接口归属

### 5.1 状态所有权表

正式施工前需补齐实际字段、锁名、构造点和 Close 路径；下表是目标责任，不是假设现在已经如此。

| 状态 | 目标所有者 | 写入者/读取者 | 必须保持 |
|---|---|---|---|
| SQLite 连接 | store；app 协调最终生命周期 | 各服务经接口调用 | 同一运行实例的连接策略、事务、关闭时机 |
| Pool 与路由权重状态 | executor | 请求执行、runtime 更新快照 | 只构造一次，策略与选择序列不变 |
| Session affinity LRU | executor | 请求执行与流完成回调 | 一小时 TTL、容量、命名空间和绑定时机按现状保持 |
| 进程表/恢复退避 | runtime | 启停、watch、恢复任务 | 一账号一套跟踪，不重复 restart goroutine |
| 模型目录与刷新去重 | 共享模型服务/runtime 分工 | 请求准备、列表、后台刷新 | 区分 pool 目录快照与 API 展示缓存，不强行合并 |
| 模型 API 展示缓存 | 共享模型服务 | gateway/console 查询 | TTL、cache key、force refresh、singleflight 语义不变 |
| 账号 quota 快照 | runtime/pool + 原持久化 | probe、console view | 配额错误不反转 readiness |
| Routing strategy/live 设置 | 设置服务与 pool | 控制台更新、执行读取 | 持久化与 live 应用顺序不变 |
| Cross-provider model pool | 单一设置状态 | 请求准备、模型视图 | gateway/console 不各存一个 atomic |
| 请求 recorder | logs | handler、executor attempt、流结束 | 一次启动、正确收尾，不重复完成 |
| 日志 ring | logs | 现有日志写入 | 容量、过滤、读取行为保持 |
| Stats cache | 日志查询服务 | overview/logs | 窗口、bucket、过期与排序不变 |
| Maintenance/update job | update 协调器 | console 命令、server 检查 | 单一 job/mutex/atomic 语义与阻断路径 |
| Provider client 缓存/登录状态 | 对应 provider client | 登录、catalog、chat | 不因新 handler 重建 client |
| 全局代理/HTTP transport cache | 原 proxy/provider 边界 | 设置、运行时、provider | 不改变连接复用、失效和 worker 重启行为 |

**状态迁移规则：先转移所有权，再删除旧字段；不能新旧两边各维护一份“暂时同步”。**

### 5.2 类型归属原则

- `Account` 等基础实体留在轻量 `accounts`，不引用 Store、Manager、Pool。
- grants 是权限语义，放 auth；迁移时先解除 auth 对相关旧 helper 的反向依赖。
- 请求日志与 attempt 数据归 logs；logs 不再直接 import 具体 store。
- Pool item、route 和分类策略归 executor。
- Provider 返回的中立错误保留在 providers 契约层。
- SQL 行类型只在 store 内部使用，HTTP DTO 不等同于数据库行。
- 类型移动先保持字段、JSON tag、零值和方法行为，不同时重新命名响应字段。
- 不抽一个涵盖所有类型的 `common/domain/types` 大包。只有实际循环无法以职责调整解决时，才单独审查一个足够小的共享契约包。

### 5.3 Go 循环依赖与兼容 alias 的限制

以下兼容方式可能不可行：

```text
accounts.Store = store.Store
store 又 import accounts.Account
→ accounts ↔ store 循环
```

同理：

```text
accounts.Pool = executor.Pool
executor 仍 import accounts
→ accounts ↔ executor 循环
```

因此：

1. 先用 `go list` 盘点真实 import 图。
2. 先解开类型和实现依赖，再迁移具体实现。
3. 对上述环路，优先原子更新调用方，不保留反向 alias。
4. 确需兼容转发时，只放在不会被新实现反向引用的上层门面。
5. Go 不能给外包类型添加新方法。迁 Store 时要一起迁移同一接收者的方法集合，不仅移动 struct。
6. 原子修改一组 import 是允许的；原子重写一组业务算法不是。

### 5.4 接口抽取方式

- 从现有调用点抽接口，定义在使用方或已有中立契约层。
- 复用当前已有 `Store`、`ProcessStarter`、`ManagedProcess` 等能力，不平行造第二套。
- 单独检查 optional type assertion：例如 provider 可能除显式 Store 接口外，还动态探测 settings 能力。新 wrapper 必须保留这些能力，否则编译通过也会改变行为。
- 接口优先覆盖一项职责，不建立一个包含所有服务方法的 `Dependencies`/`Repository`。
- 构造函数接收明确依赖；handler 不能接收整个 app 再到处取 Store/Manager。
- 不增加容器、反射注册、泛型 CRUD、事件总线来解决简单依赖问题。
- 不先把所有签名改成“理想设计”，以最小边界拆分为准。

### 5.5 共享模型能力的特殊边界

必须分别识别：

1. provider 返回的原始模型目录；
2. runtime/pool 的可服务模型快照与 proven membership；
3. API 聚合后的展示缓存；
4. SQLite 中 per-model 默认设置；
5. 请求时 reasoning/context 默认值的应用。

这些有关联，但不是同一份数据，不能趁重构合并缓存或统一刷新。

建议共享服务组合上述能力，handler 不各写一套聚合。executor 只依赖它需要的目录准备和默认设置读取接口，不直接依赖 `control` 的整个服务对象或 SQLite。

---

## 6. 必须保持的行为契约

### 6.1 HTTP 与静态资源

- 路径、HTTP 方法、query/header/body 的解释保持。
- `/api/providers` 和账号/logs/model 子路径的内部 action 不遗漏。
- key 类型、权限、region grants、console/client 的差异保持。
- CORS preflight 在原顺序回答；不能把 OPTIONS 放到 auth 后面。
- maintenance 检查位置和 `blocksDuringUpdate` 的路径判定保持。
- 不更换 ServeMux，不借机使用新的 method-pattern 改变 404/405/重定向。
- 保留 webui 静态路径与允许的 SPA fallback 页面，不扩大成任意路径都返回 index。
- 请求 body 限制、JSON 解码方式、空值处理和错误文本按当前表现固定。

### 6.2 请求处理与调度

以 `REQUEST.md` 为准，并以基线测试固定实现细节：

- 显式 pin → session affinity → pool 的优先级。
- `X-Qoder-Account` 历史名称对所有现有 provider 的作用保持。
- sticky escape、provider/region 限制、grants、并发、readiness/cooldown 检查保持。
- Session seed 的结构化 role-tagged hash、无 user anchor 的退化路径、显式 session 优先级保持。
- LRU 只保留 hash → account ID，不增加原文存储。
- 成功后绑定；流式何时判定成功与现有 CommitSession 时机保持。
- round-robin、weighted-round-robin、fill-first 及权重归一化保持。
- `Models == nil` 与空列表、catalog + proven models 不能因 DTO 转换丢失区别。
- 模型级冷却与账号级冷却分开，backoff kind/version 语义不变。
- quota、rate_limit、auth、not_ready、model_not_available、unavailable、invalid_request 的规则不重写。
- 尤其不能把所有 429 当成余额耗尽，也不能把无 failover 的失败改成自动切换。
- 客户端取消不变成普通上游错误或额外冷却，按当前处理路径验证。

### 6.3 模型、reasoning 与 provider

- 模型公共 ID、provider 前缀、region 和 settings key 的映射保持。
- Qoder CN 仍是 `provider=qoder + region=cn`。
- reasoning 始终 catalog-driven，控制台值只是 default。
- 客户端更高合法 reasoning 不被 default 限制。
- onlyReasoning 与 DeepSeek high 等已有锁定语义保持。
- 不为 WorkBuddy 增加 Trae Max/context-window 开关。
- Qoder worker 的 HTTP/SSE 请求格式、鉴权、超时、重定向、错误解码不改变。
- 不升级 pinned CLI，不改 hooks，不改 worker HOME/端口分配规则。

### 6.4 SSE 与日志

- 各协议事件结构、顺序、终止标记、错误映射保持。
- Scanner 限制、flush、usage 聚合、TTFB/first-token 计时起点保持。
- 区分上游未成功、流已开始后失败、客户端写失败和正常完成。
- response Body 所有权明确；取消与关闭不会重复或遗漏。
- 每个 attempt 的编号、状态、错误分类和请求最终行保持。
- `[DONE]`、EOF、工具输出和空流的现有处理方式固定。
- 不强行统一三种协议的错误 body 或 SSE 文本格式。
- 不将操作系统 TCP 分包边界作为固定协议契约，但应用 flush 行为需验证。

### 6.5 SQLite 与文件

- 历史 migration 的 filename/order/SQL 原始字节均保持。
- 不仅 tabs/spaces，raw string 内注释与换行也参与兼容约束。
- 数据库路径、默认 data dir、凭据格式、HOME、文件权限和导入导出格式保持。
- 保留事务边界、锁、连接 PRAGMA、时间存储格式和排序。
- 本轮原则上无 schema migration；如确有结构需求，暂停并独立审批。
- SQLite 在线备份沿用已有安全实现，不直接复制正在写入的 `.db` 当作完整备份。

### 6.6 生命周期与更新

- `cmd/server` 的 dotenv、环境变量/配置优先级、bind address 和超时保持。
- app 的创建不代表可以顺便将 panic 改 error、同步启动改异步启动。
- 当前存在 manager.Start、provider 注册/注入、刷新循环等先后关系；先记录实际顺序，不按理想架构擅自重排。
- 账号 create/update/delete/import 的校验、落库、pool、runtime 副作用顺序保持。
- quota 失败不能因为新的统一 Probe 接口使 Ready 翻转。
- Checkin/keepalive 的 opt-in、本地时区、日期边界、补偿和 retry 行为保持。
- maintenance、prepare/apply/cancel/rollback、任务互斥和 agent 状态接管保持。
- `cmd/updater`、`internal/updater` 行为不变，不在本轮顺便整理 host 执行器。

---

## 7. 实施阶段总览

### 7.1 任务勾选与进度更新规则

**必须边执行边更新任务状态，不要等整个重构结束后一次性补勾。写完本文不代表任何重构任务已完成。**

统一使用 Markdown 任务列表：

| 写法 | 含义 | 使用条件 |
|---|---|---|
| `- [ ] 任务` | 未开始 | 尚未执行 |
| `- [ ] 任务（进行中：……）` | 正在处理 | 尚未满足完成条件 |
| `- [ ] 任务（待验证：……）` | 实现已做，验证未完成 | 不能因代码已写好就打勾 |
| `- [ ] 任务（阻塞：原因；解除条件：……）` | 无法继续 | 写清阻塞原因与下一步 |
| `- [ ] 任务（不适用/获准延期：原因；确认人：……；日期：……）` | 经确认不在当前验收范围 | 不是已完成，保留未勾选 |
| `- [x] 任务` | 已完成并验证 | 有可核对的结果和证据 |

具体规则：

1. **逐项完成、逐项打勾。** 完成第 8 节或其他章节中的任务后，将对应 `[ ]` 改成 `[x]`，保留原任务说明，不删除任务掩盖遗漏。
2. **完成条件与任务性质对应。** 代码任务需相关测试通过；文档/盘点任务需实际产出并检查；真实账号或部署验收需实际执行且获授权。不能用单元测试通过代替未执行的真实环境验收。
3. **每次停止工作前更新。** 记录已完成、进行中、待验证与阻塞项；跨会话继续时先读取记录，不凭上一轮口头总结推测进度。
4. **证据就近记录。** 在任务下方添加缩进说明，或引用本阶段验收记录，写明日期、实现 SHA/PR、运行命令、结果和必要的脱敏证据。不要求为每项重复粘贴整段日志。
5. **一个复选框不要包含含糊的半完成。** 如任务有多项可独立验收的内容，先拆成子任务；子任务未全部完成，父任务不打勾。
6. **阶段完成比代码完成更严格。** 第 7.2 节阶段复选框只在本阶段适用任务完成、验收通过、回滚方式明确、临时依赖已记录后勾选；如需合入，以阶段验收的合入要求为准。
7. **例外不能伪装成完成。** 不适用/延期项需明确确认人、理由和影响；真实账号验收等门槛需负责人明确接受限制才能进入下一阶段。阶段摘要必须带上例外，不能写成“全部验证通过”。
8. **回归和回滚要撤销勾选。** 已完成任务因后续修改失效或被回滚时，恢复 `[ ]`，注明原因和受影响阶段；保留历史验证记录，不覆盖失败证据。
9. **有序步骤也受同一规则约束。** 第 8 节的编号步骤规定顺序，复选框记录完成；若某编号步骤没有对应复选框，执行时补成子任务，不只在聊天中说已完成。
10. **每次汇报与文档一致。** 汇报当前阶段、此次勾选项、测试结果、未验证项和下一步；未经授权不自动跨阶段、合并或部署。

记录示例（只是格式示例，不是当前实际进度）：

```markdown
- [x] 提取目标 helper，保持原控制流。
  - 验证：YYYY-MM-DD；实现 SHA/PR：<引用>；命令：<实际命令>；结果：通过。
- [ ] 验证流式取消（待验证：尚未运行断连用例）。
- [ ] 真实账号验收（阻塞：未获测试账号授权；解除条件：负责人确认隔离账号）。
```

### 7.2 阶段验收进度

以下是阶段级摘要，详细完成情况以第 8 节任务和阶段验收记录为依据。**不因文档已编写而勾选；S00 已按第 8 节任务与验收记录勾选。**

- [x] S00：基线、规则、入口与状态所有权盘点完成并验收。
- [x] S01：行为保护测试补齐，基线结果与限制已记录。
- [x] S02：api 同包拆文件完成，行为回归通过。
- [x] S03：accounts 同包拆文件完成，生命周期回归通过。
- [ ] S04：类型/接口边界整理完成，无循环依赖。
- [ ] S05：Store 迁移完成，历史 SQL 摘要与旧库兼容通过。
- [ ] S06：control 操作迁移完成，副作用顺序验证通过。
- [ ] S07：runtime 迁移完成，任务与资源所有权验证通过。
- [ ] S08：Qoder 具体实现归位，上游交互与启动配置验证通过。
- [ ] S09：Qoder Adapter 分能力接线完成，兼容验证通过。
- [ ] S10：调度职责迁移完成，选号/冷却/状态持久化验证通过。
- [ ] S11：共享请求准备与模型服务完成，所有入口行为验证通过。
- [ ] S12：gateway 迁移完成，HTTP/SSE 契约验证通过。
- [ ] S13：console/update 迁移完成，控制台与更新契约验证通过。
- [ ] S14：server/app/cmd 接线完成，启动关闭与进程级验证通过。
- [ ] S15：过渡层清理、依赖守卫、文档和最终验收完成。

### 7.3 阶段范围、依赖与风险

| 阶段 | 工作内容 | 主要依赖 | 风险 |
|---|---|---|---|
| S00 | 同步、盘点、确认规则和状态所有权 | 无 | 低 |
| S01 | 基线与行为保护测试 | S00 | 低，但需防真实副作用 |
| S02 | api 同包拆文件 | S01 | 低 |
| S03 | accounts 同包拆文件 | S01 | 低 |
| S04 | 类型/接口边界、循环依赖整理 | S02/S03 | 中 |
| S05 | SQLite Store 迁移 | S04 | 高 |
| S06 | control 账号/设置操作边界 | S05 | 中高 |
| S07 | runtime 生命周期边界 | S06 | 高 |
| S08 | Qoder 具体实现归位 | S07 | 高 |
| S09 | Qoder Adapter 接入 | S08 | 高 |
| S10 | Pool/route/classify 调度职责归位 | S04、S07；建议 S09 后 | 高 |
| S11 | 共享请求准备与模型服务 | S06、S10 | 高 |
| S12 | gateway 迁移 | S11 | 中高 |
| S13 | console/update HTTP 迁移 | S06、S11、S12 | 中高 |
| S14 | server/app/cmd 最终接线 | S07、S12、S13 | 高 |
| S15 | 清理、依赖守卫、总体验收 | S14 | 中 |

默认按顺序实施。S02/S03 的方案分析可并行，合入时仍分别验收。涉及相同共享对象的结构变更不要并行落地。

**依赖调整允许，但必须先更新理由。** 例如 S07 若只能通过引入 `runtime ↔ accounts` 环路推进，应先前置 S10 的最小类型/接口切割，而不是用全局变量绕过。整套顺序服务于无循环和可验证，不是机械流水线。

---

## 8. 阶段 S00—S15：逐步实施

### S00：建立施工基线

**目标：知道从哪一版开始、有哪些入口、哪些状态只能有一个实例。**

执行：

- [x] 阅读 AGENTS、ARCHITECTURE、REQUEST、PLAN、DEVELOPMENT 的现行内容。
  - 验证：2026-09-18；基线 SHA：`624874a`；结果：现行边界仍是 `auth / endpoint / executor / translate / api`；PLAN 即时门槛仍是 L6 / WorkBuddy / Trae T5 真实验收，重构不替代它们。
- [x] 检查 `git status --short`、当前分支、worktree 和未提交修改。
  - 验证：开工时本仓在 `fix/devin-codex-tools-and-compact`（`9c01479`，与当时 origin 同步），仅未跟踪 `docs/REFACTORING.md`；`main` 被 worktree `qoder-api-proxy-overview` 占用（落后 origin/main）。
- [x] 按仓库规则获取最新 main，合并到当前工作分支；已是最新 main 时跳过。
  - 验证：`git fetch origin`；`origin/main` = `624874a`（#189）。本仓 `main` 被其他 worktree 占用，未在此 checkout；从 `origin/main` 建分支，等价于已是最新 main。
- [x] 如 main 在其他 worktree，不切换占用 main，不替其他 worktree处理工作区。
  - 验证：未 `git checkout main`；未改动 `qoder-api-proxy-overview` 或其他 worktree。
- [x] 发生冲突先解决并复测，不边冲突边重构。
  - 验证：从 `origin/main` 新建分支，无合并冲突。
- [x] 在干净、明确的基线上创建或确认重构分支。
  - 验证：`refactor/s00-baseline` @ `624874a`；工作区仅未跟踪本手册（随后纳入本阶段文档提交）。
- [x] 记录基线 SHA、工具链版本、现有测试结果。
  - 验证：见下方「S00 施工基线」。
- [x] 完成全路由和后台入口盘点，包括 handler 内 action。
  - 验证：见下方「S00 路由与入口盘点」；对照 `internal/api/server.go` `routes()`、`handleAccountByID`、`handleLogs`、worker/updater 实际代码。
- [x] 完成状态所有权表：构造点、锁、写入方、关闭方。
  - 验证：见下方「S00 状态所有权」；对照 `api.New`、`Manager.Start`/`Close`、`Server.Close`。
- [x] 确认重构里程碑优先级，不绕过 PLAN 中真实账号验收门槛。
  - 验证：本地 `docs/PLAN.md` 即时门槛仍是 Phase L6、WorkBuddy 验收清单、Trae T5。本轮只整理已实现的 Qoder / WorkBuddy / Trae / Devin；Messages 是已有入口协议，不等于接入 Anthropic provider。重构 PR 默认不合入真实账号或托管更新验收。`PLAN.md` / `ARCHITECTURE.md` 按 `.gitignore` 为本地文件，不进入 git；可追踪的阶段摘要写在本手册，并已同步到本机 PLAN。
- [x] 在进入跨包阶段前，同步批准后的 AGENTS/ARCHITECTURE 边界说明。
  - 验证：现行批准边界仍是 `auth / endpoint / executor / translate / api`。已跟踪的 `AGENTS.md` 文档表指向本手册，并写明目标目录在跨包阶段获准前不生效。本机 `ARCHITECTURE.md` 同步了同样说明。S04 之前不得声称旧边界失效。

只读盘点可用：

```bash
git status --short
git branch --show-current
git worktree list
git rev-parse HEAD
go version
node --version
npm --version
go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/...
```

分支同步按实际工作区执行，不提供带 `reset --hard`、自动 stash 或强制切换的流水线。

**交付：** PLAN 中的里程碑入口、实际路由/生命周期盘点、测试基线摘要。

**通过：** 工作区归属明确，没有未知失败，没有未确认的数据/生产操作。

**回滚：** 本阶段只有文档与记录，可独立撤销；不要撤销他人的已有工作。

#### S00 施工基线

| 项 | 值 |
|---|---|
| 日期 | 2026-09-18 |
| 分支 | `refactor/s00-baseline` |
| 基线 SHA | `624874a0331f5e8f012ef104b633e69826487458`（`origin/main`，#189） |
| 文档观察点 | 编写手册时为 `9c01479`；实施前已重新核对，当前 main 已包含该提交 |
| Go | `go1.25.6 darwin/arm64` |
| Node | `v22.19.0` |
| npm | `10.9.3` |
| `main` worktree | `/Users/cj/Documents/personal/project/qoder-api-proxy-overview` 占用本地 `main`（落后 origin）；本仓未 checkout `main` |
| 本仓工作区 | 仅本阶段文档；无代码行为变更 |

测试基线（未改预期）：

```text
go test ./...     通过（无未知失败）
go vet ./...      通过
go build ./cmd/server ./cmd/updater   通过
(cd worker && npm test)   55/55 通过
```

未运行：`go test -race ./...`（S00 不涉及并发改动）；`frontend` `npm ci/build/lint`（本轮未改 UI）；真实账号与托管更新（PLAN 门槛，opt-in）。

现行批准包边界仍是 `auth / endpoint / executor / translate / api`。`api.New` 仍组装 Store、Manager、providers、executor、logs、update。目标 `app / server / gateway / console / control / runtime / store` 在跨包阶段获准前不生效。

当前内部 import（生产包，测试例外另计）：

- `api` → accounts, auth, endpoint, executor, logs, providers(+devin/trae/workbuddy), proxy, translate, update, webui, config, buildinfo
- `accounts` → providers, proxy, sqlite
- `auth` → accounts
- `executor` → accounts, endpoint, providers, translate
- `logs` → accounts
- `providers/<name>` → accounts, providers, proxy, translate
- `updater` → update, buildinfo
- `app` 包尚不存在；底层包不引用 cmd

无 `accounts ↔ store` 环（store 尚未独立）。已有耦合：`auth`/`logs`/`providers/*`/`executor` 都直接 import `accounts`。S04 必须先切开这些类型/接口，才能迁 Store。

#### S00 路由与入口盘点

中间件顺序（`Server.Handler`，`internal/api/server.go`）：

1. 仅对 `/v1/models`、`/v1/chat/completions`、`/v1/messages`、`/v1/responses` 写 OpenAI CORS。
2. 上述四路径的 `OPTIONS` 直接 204，不进 mux / auth / maintenance。
3. `maintenance && blocksDuringUpdate(path)` → 503 `service_updating`。
4. `ServeMux`；鉴权在路由包装上（`withAPIKey` / `withConsoleKey`）。

`blocksDuringUpdate`：不阻断 `/health` 与 `/api/system/update{,/prepare,/apply,/cancel,/rollback}`；阻断其余 `/api/*` 与 `/v1/*`。静态与 SPA 不阻断。控制台 `/api/*` 的 OPTIONS **不是** CORS 预检，测试预期 401。

ServeMux 无 method pattern；方法过滤在 handler 内，或完全不检查。

| 方法 | 路径 / 内部 action | 鉴权 | Handler | 更新阻断 | 备注 |
|---|---|---|---|---|---|
| 任意 | `/health` | 开放 | `handleHealth` | 否 | |
| OPTIONS | 四个 `/v1` 路径 | 开放 | `Handler` 短路 | 否 | CORS 204 |
| 任意 | `/v1/models` | client 或 console | `handleModels` | 是 | 无 `refresh`；`fetchWorkerModels(false)` |
| POST | `/v1/chat/completions` | client 或 console | `handleChatCompletions` | 是 | pin：`?account=` 或 `X-Qoder-Account` |
| POST | `/v1/messages` | client 或 console | `handleAnthropicMessages` | 是 | |
| POST | `/v1/responses` | client 或 console | `handleResponses` | 是 | |
| POST | `/api/chat` | console | 同上 `handleChatCompletions` | 是 | 同一执行路径 |
| 任意 | `/api/overview` | console | `handleOverview` | 是 | `?refresh=1` → `RefreshAll(..., true)` |
| 任意 | `/api/overview/summary` | console | `handleOverviewSummary` | 是 | 不 refresh |
| GET, POST | `/api/system/update` | console | `handleSystemUpdate` | 否 | GET `?force=1`；POST 一次 apply |
| 任意 | `/api/system/update/prepare` | console | `handleSystemUpdatePrepare` | 否 | 不检查 Method |
| 任意 | `/api/system/update/apply` | console | `handleSystemUpdateConfirm` | 否 | 不检查 Method |
| POST | `/api/system/update/cancel` | console | `handleSystemUpdateCancel` | 否 | |
| POST | `/api/system/update/rollback` | console | `handleSystemUpdateRollback` | 否 | body `{version}` |
| GET, PATCH | `/api/system/settings` | console | `handleSystemSettings` | 是 | |
| GET, POST | `/api/system/console-key` | console | `handleConsoleKey` | 是 | POST 需 `{rotate:true}` |
| GET, POST | `/api/keys` | console | `handleAPIKeys` | 是 | |
| GET, PATCH, DELETE | `/api/keys/{id}` | console | `handleAPIKeyByID` | 是 | id 不含 `/` |
| 任意 | `/api/models` | console | `handleModelsAPI` | 是 | `?refresh=1`、`?view=regional`、`?account=`；5min cache |
| GET, PATCH | `/api/models/{id}` | console | `handleModelSetting` | 是 | 前缀或 `?provider=` |
| GET | `/api/providers` | console | `handleProviders` | 是 | |
| GET, POST | `/api/accounts` | console | `handleAccounts` | 是 | GET `?refresh=1` |
| POST | `/api/accounts/import` | console | `handleAccountImport` | 是 | `qoder-native-v1` / `workbuddy-oauth-v1` / `trae-oauth-v1` / `devin-session-v1` |
| GET | `/api/accounts/{id}` | console | `handleAccountByID` | 是 | |
| PATCH | `/api/accounts/{id}` | console | 同上 | 是 | |
| DELETE | `/api/accounts/{id}` | console | 同上 | 是 | 204 |
| POST | `/api/accounts/{id}/refresh` | console | 同上 | 是 | 先于 provider-native；`?quota=1` |
| GET | `/api/accounts/{id}/checkins` | console | 同上 | 是 | WorkBuddy only |
| POST | `/api/accounts/{id}/checkin` | console | 同上 | 是 | WorkBuddy only |
| POST | `/api/accounts/{id}/login/device` | console | 非 qoder：`StartLogin`；qoder：proxy `/admin/login/device` | 是 | qoder 等 `hasAuthManager`，转发 Method |
| GET | `/api/accounts/{id}/login/status` | console | 非 qoder：`PollLogin`；qoder：proxy + `oauth_if_complete` | 是 | |
| POST | `/api/accounts/{id}/login/callback` | console | 非 qoder `CompleteLogin` | 是 | qoder → 404 unknown action |
| GET | `/api/accounts/{id}/export` | console | 分 provider | 是 | |
| POST 转发 | `/api/accounts/{id}/login/pat` | console | qoder proxy `/admin/login/pat` | 是 | 非 qoder 404 |
| POST 转发 | `/api/accounts/{id}/rewarm` | console | qoder proxy `/admin/rewarm` | 是 | 非 qoder 404 |
| GET | `/api/logs/requests` | console | `handleListRequestLogs` | 是 | account/status/error_kind/model/id/q/stream/limit/offset/from/to |
| DELETE | `/api/logs/requests` | console | `handleClearRequestLogs` | 是 | |
| GET | `/api/logs/requests/{id}` | console | `handleGetRequestLog` | 是 | |
| GET | `/api/logs/runtime` | console | `handleRuntimeLogs` | 是 | after/limit/offset/level/q/account |
| GET | `/api/logs/stats` | console | `handleRequestStats` | 是 | hours∈{1,24,168} 否则 24；10s cache |
| GET `/api/logs` 及其他 | `/api/logs` `/api/logs/` | console | `handleLogs` default | 是 | 404 `unknown logs endpoint` |
| FileServer | `/assets/` 与 listed icons/manifest | 开放 | `webui.Handler()` | 否 | |
| 任意 | `/` 与 SPA allowlist | 开放 | `HandleFunc("/")` | 否 | allowlist：`/login` `/auth` `/providers` `/access` `/accounts` `/system` `/logs` `/keys`；其它 404，不扩大 fallback |

账号 action 分派顺序（负载相关，`handleAccountByID`）：CRUD → `refresh` → `checkins` → `checkin` → 非 qoder login/export → qoder worker proxy。

其它 HTTP 进程（不在 proxy mux 上，本轮不改行为）：

- Qoder worker `daemon.mjs`：开放 `/health`；worker key 保护 `/admin/models|quota|login/*|rewarm` 与 chat。
- `cmd/updater`：`/health`、`/v1/status|prepare|apply|cancel|update`。
- Trae `127.0.0.1:0` `/authorize`、Devin `/callback` 本地登录捕获。

已删除且测试钉死的路径：`/api/login/status|device|pat`、`/api/rewarm`、`/debug/auth-snapshot`、`/debug/endpoints`。

#### S00 状态所有权

`api.New` 顺序（必须保持，不得按理想架构重排）：

1. 解析 DataDir（空则 `{QoderHome}/.proxy-data`）
2. `accounts.OpenStore(dataDir/qoder.db)`（mkdir、`MaxOpenConns(1)`、PRAGMA、migrate；失败 panic）
3. `ensureProxyAPIKey` / `ensureProxyURL` / `ensureCrossProviderModelPool` / `ensureRoutingStrategy` / `ensureWorkBuddyCheckinTime`
4. 解析 RuntimeDir（空则 `/tmp/cli2api-runtime`）
5. `logs.NewRing(2000)`；`log.SetOutput(stderr+ring)`
6. `accounts.NewManager`（内部立刻 `go drainCooldowns`，空 Pool，默认 `ExecStarter`）
7. `manager.Start`：列出账号 → 启用账号 `startAccountWithRecovery` → `restoreCooldowns`
8. `pool.SetRoutingStrategy`
9. `providers.NewRegistry`；注册 WorkBuddy / Trae / Devin client（同一 `*Store`）；`SetProviders` / `SetWorkBuddy`。Qoder 不是 in-process adapter。
10. `go manager.RefreshAll(context.Background(), false)`（**不**绑 `runCtx`）
11. `logs.NewRequestRecorder`（启动 `loop`）；`go PurgeLoop(stopLogs, 1h)`；`go RunWorkBuddyMaintenanceLoop(stopLogs)`
12. update Checker + Unix 或 HTTP agent
13. `executor.NewChatExecutor`（session LRU 1h / 10k）；MaxAttempts、Providers、OnAttempt
14. 组装 Server、atomic 交叉池开关、`routes()`

`cmd/server`：godotenv → `config.Load` → `api.New` / `defer Close` → `ReadHeaderTimeout=10s` `IdleTimeout=120s`（无 Read/WriteTimeout）→ SIGINT/TERM → `Shutdown(10s)` → `Server.Close`。`cmd/updater` 本轮不动。

`Server.Close`：`close(stopLogs)` → `manager.Close` → `Store.Close`。

`Manager.Close`：停 persist observer → 等 drainer → `cancel(runCtx)` → 等 recovery → 停子进程。

当前未关闭（保持现状，不在本轮“顺便修好”）：`RequestRecorder.loop`（queue 从不关闭）；log ring / `log.SetOutput`；provider `TransportCache.CloseIdleConnections`；Trae/Devin login listener；`ChatExecutor.HTTPClient` / `Manager.httpClient`；`New` 里那次 `RefreshAll(Background)`。

| 状态 | 当前所有者 | 构造点 | 锁 | 写入者 | 读取者 | 关闭 | 必须保持 |
|---|---|---|---|---|---|---|---|
| SQLite | `accounts.Store.db`；`Manager.store` | `OpenStore` ← `api.New` | 无 Store mutex；1 conn + busy_timeout=5s | Store 方法、persist drainer、recorder | Manager / API / auth / providers | `Server.Close` 在 Manager.Close 之后 `db.Close` | 同一实例、同一事务/关闭时机 |
| Pool + 权重 | `Manager.pool`；Server/executor 别名同一实例 | `NewPool` ← `NewManager`；策略稍后 `SetRoutingStrategy` | `Pool.mu`；observer 在 unlock 后 | Upsert/Remove/SetWeight/SetRoutingStrategy/Mark*/Merge*；PickRoute 写计数器 | PickRoute、overview、models、executor | 无 Close；进程退出丢失内存 | 只构造一次；策略与选择序列不变 |
| Session LRU | `ChatExecutor.SessionAffinity` | `NewChatExecutor` | `SessionAffinity.mu` | Bind/Forget/RecordEscape/Get | Get、settings Stats | 无；重启清空 | 1h TTL、容量、hash→account |
| 进程表/恢复 | `Manager.processes/restarts/restartBackoff/recovering` | `NewManager`；watch/recover goroutine | `Manager.mu`；persistMu；proxyReloadMu | start/stop/recovery | AccountURL、Close、proxy reload | Close：停 persist → cancel → Stop 子进程 | 一账号一套跟踪 |
| 目录快照 | Pool `Item.Models/ProvenModels`；WB/Trae `Client.catalog`；Devin **包级** cache | refresh / Models() | Pool.mu；Client.mu；Devin `catalogMu` | MergeModels、MarkOK、RemoveModel | routing `ItemHasModel`、display | 无；Ensure 用 `runCtx` | 不与 API 展示缓存合并 |
| API 展示缓存 | `Server.modelsAPICache` TTL 5m | 惰性 | `modelsAPICacheMu` | stale-while-revalidate goroutine | `/api/models`、overview peek | 无 | TTL、key（account@merged\|regional）、singleflight |
| Quota 快照 | Pool `Item.Quota` + SQLite `SaveQuota` | refresh / check-in / start Upsert | Pool.mu | fetchQuota / fetchProviderQuota | routing `Quota.Exceeded`、AccountView | 随 Store/进程 | 失败不翻转 Ready |
| 路由策略/live 设置 | Pool.routingStrategy + secret；ProxyURL live 在 Manager/starter | ensure* 然后 SetRoutingStrategy | Pool.mu；`settingsMu`；proxyReloadMu | PATCH settings；ReloadProxyURL；console-key rotate | PickRoute、GET settings | 无 | 落库与 live 顺序不变 |
| 交叉模型池 | `Server.crossProviderModelPool atomic.Bool` + secret | ensure 默认 `"1"` | atomic；PATCH 用 settingsMu | PATCH | chat 前缀门、health/overview | 无 | 不各存一份 |
| Request recorder | `logs.RequestRecorder` queue 256 | `NewRequestRecorder` 即起 loop | channel；满则丢 | Start/Finish/Attempt/Usage/StreamDiagnostic | HTTP 读 Store 不读 queue | PurgeLoop 随 stopLogs；**loop 不关** | 一次启动、正确收尾、丢队列是现行为 |
| 日志 ring | `applogs.Ring` 2000；Server.ring | `NewRing`；stderr+ring | Ring.mu；nextID atomic | std log + worker prefix writer | `/api/logs/runtime` | 无 | 容量与过滤 |
| Stats cache | `Server.statsCache` 10s | 惰性 | `statsCacheMu` | handleRequestStats | GET `/api/logs/stats` | 无 | 窗口/bucket/过期 |
| Maintenance/job | Server `maintenance`/`updateRunning`/`updateJob` + Checker/Agent | New 构造 checker/agent；job 在 prepare/apply | `updateMu`；atomics | update handlers | Handler 503、GET info | Close 不清理；终态路径清 flag | 单一 job/mutex/atomic |
| Provider client | Registry + 三 client 的 login/catalog | New 注册 | Registry RWMutex；Client.mu | login/catalog/SaveCredential | executor、Manager、HTTP login | 无 Client.Close | 不因新 handler 重建 |
| 代理 transport | 每 client `proxy.TransportCache` LRU 32；全局 URL 在 secret+Manager+starter | 惰性 Get | cache.mu；starter.mu | Get 插入/驱逐；ReloadProxyURL | provider httpClient | CloseIdle 只在驱逐；Server.Close **不**调 | 复用与失效语义 |
| 其它 | `Server.auth`、mux、stopLogs；`Manager.providers/workbuddy`；persist drainer | New / NewManager | verifier 无锁（rotate 换结构） | key rotate；SetProviders 一次 | HTTP auth、maintenance | stopLogs + persist wait | persist 是冷却持久化唯一路径 |

生命周期后台任务：`drainCooldowns`（事件驱动）；每 Qoder 子进程 `cmd.Wait` + `watchAccount`；失败 `recoverAccount`（1s…1m 指数，cap 8）；recorder loop；PurgeLoop 1h / 7d 或 20_000 行；WorkBuddy 维护（轮询 cap 1m；签到默认 09:00、重试 21:00、keepalive 22:00，北京时间）；启动一次性 `RefreshAll`。**没有**全池 health/quota/catalog 周期 ticker；之后靠 console refresh、chat `EnsureModelCatalogs`（5m）、`/api/models` cache。

PLAN 优先级：即时门槛仍是 L6 / WorkBuddy / Trae T5 真实验收。重构不得绕过它们，也不得开始 Cursor / Anthropic provider。S00 本身无生产副作用。

#### S00 阶段验收

```text
阶段编号：S00
验收日期与确认人：2026-09-18；执行记录写入本手册
本次勾选的任务：S00 全部执行项与 7.2 阶段摘要
未勾选任务、例外批准与影响：无 S00 延期项。真实账号/托管更新/race/前端不在本阶段范围
阶段复选框是否允许勾选：是（文档与盘点；无代码行为变更）
合入/候选 SHA：分支 refactor/s00-baseline，起点 624874a
完成的职责迁移：无
保留的临时依赖：现行 api/accounts 大包；目标目录未落地
测试证据：go test ./...、go vet ./...、go build ./cmd/server ./cmd/updater、worker npm test 55/55
真实环境验收证据：未执行（未授权，且 S00 不需要）
未覆盖项与接受人：race、frontend lint/build、真实账号、托管更新 — 记录为后续阶段/PLAN 门槛
可追踪文件：AGENTS.md、docs/REFACTORING.md。本机另有未跟踪的 PLAN.md / ARCHITECTURE.md 摘要同步（.gitignore）
是否允许进入下一阶段：S01（补行为保护测试）可开始；不自动开工
失败时回退到哪个已验收节点：撤销本阶段文档提交即可；代码基线仍是 624874a
```

### S01：补行为保护测试

**目标：先从原入口锁住行为，再改变代码位置。**

执行：

- [x] 运行已有 Go/worker 测试和 vet，记录失败而不是修改预期掩盖失败。
  - 验证：2026-09-18；分支 `refactor/s01-behavior-tests`；命令：`go test ./...`、`go vet ./...`、`go build ./cmd/server ./cmd/updater`、`cd worker && npm test`；结果：通过，未改预期掩盖失败。
- [x] 使用现有 test helper、fake worker/provider 和临时数据库，不重复造测试框架。
  - 验证：复用 `api.New`、`newCompatibilityServer`、`accounts.OpenStore`、`fakeStarter`、`updateCheckerStub`/`updateAgentStub`、`applogs.NewRequestRecorder`；无新测试框架。
- [x] 补鉴权、CORS、maintenance、路由方法与 fallback 测试。
  - 验证：`internal/api/s01_http_contract_test.go`（H01–H05）。
- [x] 补三种协议 + console chat 的关键请求/响应契约。
  - 验证：OpenAI 非流式/流式与 `/api/chat` 经 `Server.Handler`；Messages/Responses 既有 `compat_test.go` 未重写；空消息/畸形 JSON 四入口。
- [x] 补 reasoning/default/grants/pin/sticky/pool 的跨层关键路径。
  - 验证：HTTP 补 named key `/api/chat` 403、region grant 不逃逸、缺失 pin 回落到 pool（现行为）。reasoning clamp / sticky seed / 三种策略仍由既有 mapping、session_affinity、pool 测试覆盖，未重写。
- [x] 补 SSE 正常结束、错误事件、断流、取消和下游写失败。
  - 验证：handler 级成功流、流开始前 HTTP 错误、缺 `[DONE]` 不重放、写失败视为 disconnect、取消关闭上游 Body。既有 `chat_usage_test.go` relay 用例保留。
- [x] 补旧数据库启动、历史 migration 摘要、导入导出、备份恢复。
  - 验证：全部 001–020 filename/order/SQL hash；备份 keep=2 修剪；失败 native import 不留账号。无独立 Restore API，记为已知限制。既有 006/007 checksum 测试未改。
- [x] 补生命周期、签到保活与更新状态机的可控 fake 测试。
  - 验证：删除发生在恢复中时不重复 spawn；apply 期间 prepare 409；失败 agent 解除 maintenance。签到/保活既有 `manager_test.go` 未重写。
- [x] 标明哪些既有测试已经覆盖，不为了增加数量重写它们。
  - 验证：见下方「S01 覆盖与限制」。
- [x] 将真实账号验收作为显式 opt-in，不放入默认测试。
  - 验证：未增加云端/真实凭据调用；PLAN L6 / WorkBuddy / Trae T5 仍是独立门槛。

测试先在旧实现上通过；随后移动实现时测试预期不变。若基线表现与文档冲突，先明确哪项契约需要独立修复，不在重构中裁定新行为。

**交付：** 可重复运行的契约测试、数据 fixture、已知限制清单。

**通过：** 关键成功/失败路径有断言；测试不会访问生产 HOME、真实凭据或对外调用。

**回滚：** 测试 PR 可单独撤销，不改生产行为。

#### S01 覆盖与限制

既有覆盖（未重写）：`auth_test.go` named/console key 与 models grants；OpenAI CORS 预检；SPA `/auth|/accounts|/login|/logs`；`compat_test.go` Messages/Responses 事件名；`system_settings_test.go` / `provider_routes_test.go` 交叉池前缀；`session_affinity_test.go` / `session_seed_test.go` pin/sticky/image-only；`classify_test.go` + executor/pool 分类与策略；`TestStateVersionOrdersAcrossDelayedObserver`；`chat_usage_test.go` relay 缺 DONE/错误事件/typed read；`manager_test.go` 启停/探测/签到；`backup_test.go` 006/007；`logs_test.go` 过滤分页；`update_test.go` prepare/cancel/adopt。

本次新增：

- `internal/api/s01_http_contract_test.go`
- `internal/api/s01_protocol_contract_test.go`
- `TestPublishedMigrationsKeepOrderedFilenameAndSQLDigest` / `TestStoreBackupPrunesOlderSnapshots`
- `TestManagerDeleteDuringRestartDoesNotLeaveDuplicateRecovery`

已知限制（不勾成已验证）：

- 无独立 SQLite Restore 入口，只锁 Backup + prune。
- 无仓库内旧版 `.db` fixture；旧 checksum 仍用 006/007 改 checksum 再打开。
- HTTP 层未再测 catalog clamp / onlyReasoning（仍在 provider mapping 测试）。
- `monitorUpdate` 2s 轮询未在默认测试中实跑整段；失败解除 maintenance 按 `finishUpdateJob` 现路径锁定。
- 缺失 `X-Qoder-Account` 回落到 pool（不是 404）；Delete 不立即清 `recovering[]`。这是现行为，不在本阶段修复。
- 真实账号、托管更新、race、frontend 未跑。

#### S01 阶段验收

```text
阶段编号：S01
验收日期与确认人：2026-09-18；执行记录写入本手册
本次勾选的任务：S01 全部执行项与 7.2 阶段摘要
未勾选任务、例外批准与影响：真实账号/托管更新/race/frontend 不在本阶段；Restore API 不存在
阶段复选框是否允许勾选：是（测试补齐且通过；无生产行为变更）
合入/候选 SHA：分支 refactor/s01-behavior-tests，起点 ed46c02
完成的职责迁移：无
保留的临时依赖：现行 api/accounts 大包
测试证据：go test ./...、go vet ./...、go build ./cmd/server ./cmd/updater、worker npm test 55/55
真实环境验收证据：未执行
是否允许进入下一阶段：S02（api 同包拆文件）可开始；不自动开工
失败时回退到哪个已验收节点：撤销本阶段测试提交；S00 文档基线仍在
```

### S02：api 同包拆文件

**目标：先看清职责，不改变 package 边界。**

建议切分：

```text
server.go        → server.go / bootstrap.go / routes.go / middleware.go
                 / health.go / overview.go
chat.go          → chat.go / chat_prepare.go / chat_stream.go / chat_usage.go
                 / model_catalog.go / model_cache.go
compat.go        → compat_messages.go / compat_responses.go / compat_stream.go
```

执行：

- [x] 全部仍使用 `package api`。
  - 验证：2026-09-18；分支 `refactor/s02-api-split`；新文件均 `package api`，无新 package。
- [x] 原函数签名、方法接收者和字段不变。
  - 验证：`Server` 字段、`New`/`Handler`/`routes`/`handleChatCompletions` 等仍是原接收者；未改导出符号。
- [x] 只调整文件位置和必要 import。
  - 验证：按职责切开 `server.go`/`chat.go`/`compat.go`；import 仅为编译所需。
- [x] 共享 helper 留在本包，不为了移动文件导出。
  - 验证：`writeErr`/`firstNonEmpty`/`relayOpenAIStream` 等仍未导出。
- [x] 测试文件可按职责搬，但断言和 fixture 不重写。
  - 验证：测试文件未搬；S01 断言保持。
- [x] 保持原控制流、defer 位置、返回值和调用顺序。
  - 验证：函数体整段移动；无新 goroutine/interface/运行时参数。
- [x] 只格式化涉及的普通 Go 文件，不运行会触碰全仓内容的大范围清理。
  - 验证：`gofmt` 仅作用于 `internal/api/*.go`；误碰的 S01 测试已还原。

**交付：** 文件能表达职责，Server 暂时仍保留原结构。

**通过：** diff 可识别为移动；S01 测试通过；没有新 goroutine/interface/运行时参数。

**回滚：** 回滚该移动提交。

实际切分：

```text
server.go        → server.go / bootstrap.go / routes.go / middleware.go / health.go / overview.go
chat.go          → chat.go / chat_prepare.go / chat_stream.go / chat_usage.go / model_catalog.go / model_cache.go
compat.go        → compat.go / compat_messages.go / compat_responses.go / compat_stream.go
```

`compat.go` 仍保留共享 alias、tool-call 解码和 prepare/finish。测试文件未搬。

#### S02 阶段验收

```text
阶段编号：S02
验收日期与确认人：2026-09-18；执行记录写入本手册
本次勾选的任务：S02 全部执行项与 7.2 阶段摘要
未勾选任务、例外批准与影响：无
阶段复选框是否允许勾选：是（同包移动；S01 测试通过）
合入/候选 SHA：分支 refactor/s02-api-split，起点 6fd061a
完成的职责迁移：api 文件按职责拆分；package 边界未变
保留的临时依赖：Server 仍持有原字段
测试证据：go test ./...、go vet ./...、go build ./cmd/server ./cmd/updater
真实环境验收证据：未执行
是否允许进入下一阶段：S03（accounts 同包拆文件）可开始；不自动开工
失败时回退到哪个已验收节点：撤销本阶段移动提交；S01 测试基线仍在
```

### S03：accounts 同包拆文件

**目标：把 CRUD、进程、恢复、刷新、维护分开阅读。**

建议切分：

```text
manager.go
manager_accounts.go
manager_process.go
manager_recovery.go
manager_probe.go
manager_catalog.go
manager_quota.go
manager_maintenance.go
```

执行：

- [x] 全部仍是 `package accounts`。
  - 验证：2026-09-18；分支 `refactor/s03-accounts-split`；新文件均 `package accounts`。
- [x] Pool、Store 和 Manager 的类型归属暂不改变。
  - 验证：`Manager`/`Pool`/`Store` 仍在 accounts；未迁类型。
- [x] 不拆 mutex、不复制进程表、不改变 channel 所有权。
  - 验证：`mu`、`processes`、`persistMu`/`persistCond`/`persistCloseCh`、`runCtx` 仍在 `Manager` 上。
- [x] 标出每个操作读写哪些状态，但不重排操作。
  - 验证：见下方状态读写表；函数体整段移动。
- [x] 标出 Qoder 专属实现与通用生命周期的分界。
  - 验证：HOME/CLI/`ExecStarter`/`watchAccount` 在 process/recovery；in-process 仍只 Upsert pool。
- [x] 标出 WorkBuddy 专属协议和维护调度的分界。
  - 验证：check-in/keepalive 仅在 `manager_maintenance.go`，经 `WorkBuddyMaintainer`。
- [x] 迁移已有测试，保持 race/并发断言。
  - 验证：测试文件未搬；`go test ./internal/accounts` 通过。

**通过：** CRUD、refresh、restart、maintenance 既有测试保持，数据库与运行时路径不变。

**回滚：** 单包移动提交回滚即可。

实际切分：

```text
manager.go                 类型、NewManager、persist drainer、Start/Close、SetProviders
manager_accounts.go        Create/Update/Delete/Import、AccountView
manager_process.go         start/stop、ExecStarter、Qoder HOME/CLI、ReloadProxyURL
manager_recovery.go        startAccountWithRecovery、watchAccount、recoverAccount
manager_probe.go           RefreshAll/RefreshAccount/refreshOne/refreshInProcess
manager_catalog.go         EnsureModelCatalogs、fetchAccountModels/fetchProviderModels
manager_quota.go           fetchQuota/fetchProviderQuota/persistQuota
manager_maintenance.go     WorkBuddy check-in/keepalive loop
```

状态读写（未重排）：

| 文件 | 读 | 写 |
|---|---|---|
| manager.go | store.List、persist dirty | persistClosed、runCtx cancel、Stop 子进程 |
| manager_accounts.go | store、pool 快照 | store CRUD；经 start/stop 副作用 |
| manager_process.go | store 凭据、config | processes、nextPort、pool Upsert/Remove |
| manager_recovery.go | store.Get、runCtx | recovering/restarts/restartBackoff、pool RuntimeState |
| manager_probe.go | pool items、worker/adapter health | MergeHealth；quota 失败不改 Ready |
| manager_catalog.go | pool ModelsAt | MergeModels |
| manager_quota.go | worker/adapter quota | MergeQuota + SaveQuota |
| manager_maintenance.go | store 账号/checkin 记录 | check-in 记录；不写 chat cooldown |

Qoder 专属：`manager_process.go` HOME/CLI/daemon env、`watchAccount`。通用生命周期：Start/Close、recovery maps。WorkBuddy 专属：`manager_maintenance.go`。in-process 探测走 probe/catalog/quota 的 adapter 分支。

#### S03 阶段验收

```text
阶段编号：S03
验收日期与确认人：2026-09-18；执行记录写入本手册
本次勾选的任务：S03 全部执行项与 7.2 阶段摘要
未勾选任务、例外批准与影响：无
阶段复选框是否允许勾选：是（同包移动；生命周期测试通过）
合入/候选 SHA：分支 refactor/s03-accounts-split，起点 d0a147c
完成的职责迁移：Manager 方法按文件拆分；类型与锁未迁
保留的临时依赖：accounts 仍聚合 Store/Manager/Pool
测试证据：go test ./...、go vet ./...、go build ./cmd/server ./cmd/updater
真实环境验收证据：未执行
是否允许进入下一阶段：S04 可开始；不自动开工
失败时回退到哪个已验收节点：撤销本阶段移动提交；S02 基线仍在
```

### S04：整理轻量类型与消费方接口

**目标：为搬 Store/Manager/Pool 消除循环依赖。**

执行顺序：

1. 列出 accounts、auth、logs、providers、executor 的实际 import 图。
2. 列出 Account、APIKey、ProviderGrant、RequestLog、RequestAttempt、Item、RouteQuery 的使用点。
3. 将基础实体与带 Store/Manager 接收者的实现区分开。
4. 先让 logs recorder 消费持久化接口，再考虑将 RequestLog 类型移入 logs。
5. 迁 grants 前解除 auth 对旧 grants helper 的引用，避免 auth/accounts 双向依赖。
6. 为 Manager 的 pool、storage、starter 能力定义最小消费方接口；能复用已有接口就复用。
7. 为 Pool 持久化通知整理中立数据输入，不把整个 Store 注入执行层。
8. 检查 provider 的显式接口与 optional type assertion 能力是否都被保留。
9. 每处理一个边界就运行 `go list ./...` 和相关测试。

- [ ] 不增加大而全的 domain/common 包。
- [ ] 不把 SQL 行类型导出作为 HTTP 返回值。
- [ ] 类型字段、tags、nil/empty 和默认值保持。
- [ ] 兼容 alias 经过 import 环路检查。
- [ ] 新接口均有具体调用方，不为“未来可能”定义空能力。

如果一次完整拆类型牵涉太多文件，允许只先注入接口，保留实体当前位置；实现搬完后再完成轻量化。不要为追求一次完成而把十几种类型全部重命名。

**交付：** 无循环的迁移路径和最小接口，调用行为尚未变化。

**通过：** 全量编译通过；可解释每个新接口的真实消费方；没有行为适配代码被偷偷加入。

**回滚：** 按接口/类型批次撤销，不与后续 Store 移动混成一个提交。

### S05：迁移 SQLite Store

**目标：数据库实现归 store，业务模块不再依赖具体 SQLite。**

执行：

- [ ] 迁移前记录全部已发布 migration 的 filename/order/原始 SQL 摘要。
- [ ] 确认 store 所需数据类型不再由数据库实现反向拥有。
- [ ] 将 Store 类型、OpenStore、同一接收者的方法集合迁到 store。
- [ ] 将 SQL helpers、扫描、事务、备份实现一并归位。
- [ ] 文件可先整体移动，再另一个 PR 同包拆 accounts/credentials/logs/settings。
- [ ] 原子更新相关构造点与 import，避免 accounts→store→accounts alias 环路。
- [ ] auth、providers、logs 等通过已有/新增消费方接口接入。
- [ ] 保留 SQLite 驱动、PRAGMA、busy timeout、连接池、序列化与错误。
- [ ] 再次计算 SQL 摘要，与迁移前逐条比较。
- [ ] 使用干净数据库和合成旧数据库分别启动、读写、重启。
- [ ] 测试导入导出、备份校验及恢复，检查敏感字段未进入快照。

禁止把“SQL 看起来一样”当成通过。禁止将历史 SQL 格式化为新的缩进。

**交付：** `internal/store`；旧 accounts 逐步收敛为轻量类型/暂存未迁移实现。

**通过：** 无新 schema、摘要一致、旧库正常打开、原事务行为测试通过。

**回滚：** 撤销代码即可恢复旧包路径；运行过后的数据回滚仍按第 11 节，不假设删除库即可解决。

### S06：提取 control 应用操作

**目标：HTTP 不再协调账号写库、运行时和 pool。**

先为每个操作填写真实顺序：

```text
校验 → 读取 → 持久化 → runtime/pool 副作用 → 返回
```

这是记录格式，不是规定所有操作必须照此顺序；实际先后以旧实现为准。

执行：

- [ ] 对 Create/Update/Delete/Import/登录完成/代理更新/key 更新列出成功和中途失败路径。
- [ ] control 初期通过窄接口调用旧 Manager，不直接重新实现进程操作。
- [ ] 先移动账号操作编排，再移动设置/key 操作。
- [ ] 保留同一临界区内的步骤，不拆出锁后产生新的中间可见状态。
- [ ] 保留落库成功但 runtime 失败时的现有返回与恢复方式。
- [ ] AccountView 组合持久化数据和实时快照，不维护第二份运行时状态。
- [ ] 查询保留 `refresh=0/1`、forceQuota 与被动展示的区别。
- [ ] 备份控制逻辑调用 store 备份能力，不复制 SQLite 备份实现。
- [ ] HTTP 错误映射暂时仍在 api，control 返回原有可识别错误。

**禁止：** 事件总线、异步 CRUD、新事务补偿、统一重试、重做参数验证。

**通过：** 每个操作的数据库/runtime/pool 状态和失败顺序与旧实现一致。

**回滚：** 保留旧窄入口期间可逐操作撤销；不要依靠并行执行新旧操作来比对。

### S07：提取 runtime 生命周期

**目标：生命周期独立，账号业务操作留在 control。**

执行：

- [ ] 迁移进程表、启动/停止/watch、restart/backoff、恢复任务。
- [ ] 迁移 probe/catalog/quota 刷新调度和关闭路径。
- [ ] 迁移 maintenance loop，但保留当前 WorkBuddy 能力调用与调度语义。
- [ ] runtime 经 starter/provider 能力调用实现，不 import 具体产品包。
- [ ] pool 尚未迁移时，通过已建立的边界使用原实现，避免双向 import。
- [ ] 构造与启动分工先复刻旧行为，不顺便实现 lazy start。
- [ ] process、goroutine、ticker、channel 各有唯一所有者。
- [ ] 代理/key 改动仍以原方式应用到既有 worker 和新 worker。
- [ ] 保持启动失败、恢复失败、账号删除与 watch 并发时的现有处理。
- [ ] 保持 shutdown 先后关系，补取消/退出测试。

**通过：** fake starter 可验证每个账号的 Start/Stop 次数；无重复维护循环；race 检查通过或原有问题被明确隔离。

**回滚：** 保持 manager 兼容入口的方向无环；按生命周期迁移提交撤销。

### S08：归位 Qoder 具体实现

**目标：Qoder 产品细节不散落在 api、runtime、executor。**

执行：

- [ ] 将 HOME 物化、CLI 路径选择、daemon 命令参数移至 `providers/qoder`。
- [ ] 将 worker catalog/quota/chat/login 通信提取为 provider 方法。
- [ ] HTTP handler 留在 api/后续 console，provider 不接收浏览器 ResponseWriter。
- [ ] runtime 持有通用 ManagedProcess/ProcessStarter，负责重启策略，不重复维护 HOME。
- [ ] 为 worker transport、返回体和失败建立 fake server 测试。
- [ ] 核对 qoder/qoderclicn 的 region、配置目录、环境变量和 worker 管理 key。
- [ ] 保留 AuthManager 等待、轮询、超时和 token 同步时机。
- [ ] 不改 worker/src 协议、不升级 CLI、不修改 compat hook。

**通过：** 同样输入产生同样 worker 请求和进程启动配置；默认测试不启动真实 CLI。

**回滚：** 旧调用点转发到搬后的实现，逐项可撤销；不在同一个提交切换整个注册架构。

### S09：接入 Qoder Adapter

**目标：统一调用边界，而不是统一所有 provider 的行为。**

执行：

- [ ] 复用现有 providers.Adapter 和 Registry。
- [ ] 用 Adapter 包住 S08 的旧行为，先不删除原调用路径。
- [ ] 分能力迁移：目录/配额/登录/chat/stream 的顺序按依赖确定。
- [ ] 用 fake upstream 对比旧调用与 Adapter 调用的参数、headers、错误、usage。
- [ ] 核对 nil adapter、unsupported capability 和默认 Qoder 路径的既有处理。
- [ ] Qoder readiness/hot 等 child 特性仍保留，不假设 in-process 同构。
- [ ] 签到/保活能力如需泛化，只整理能力名；不让未支持 provider 获得新行为。
- [ ] 所有能力验收后再删除旧 executor/workerproxy 产品分支。

旧路径保留仅供代码迁移和离线测试，不能给生产增加新旧双写、双请求或额外客户端开关。

**通过：** Registry 接线改变，但 fake 捕获的上游交互一致；真实账号测试另行 opt-in。

**回滚：** 可撤销某项能力切换，不必撤销 S08 的机械代码移动。

### S10：迁移 Pool、route、classify

**目标：账号注册表不再拥有请求调度。**

执行：

- [ ] 把 Pool/Item/RouteQuery/ItemHasModel 及策略迁入 executor 的调度职责。
- [ ] 移动错误分类及 Retry-After/退避 helper 时保持所有判断顺序。
- [ ] provider 使用中立 Error/ClassifiedError；不 import executor taxonomy 实现。
- [ ] 保留同一 Pool 实例，不因 runtime/control 各构造一次产生两套状态。
- [ ] 保留 in-flight 增减、成功/失败观察和 rewarm 调用时机。
- [ ] 保留账号/模型冷却、kind、backoff 和 StateVersion 顺序。
- [ ] 将持久化观察器接到原写入队列，保留队列排序、drain 与关闭方式。
- [ ] 原子更新调用点，避免 accounts.Pool alias 反向引用 executor。
- [ ] 对固定候选和请求序列比较选择轨迹，不只比较最终 HTTP 成功率。

**通过：** 三种策略、sticky、pin、grants、catalog/proven、模型冷却和持久化顺序测试通过。

**回滚：** pool 包归属迁移独立于算法；回滚代码不改变已持久化格式。

### S11：提取共享请求准备与模型服务

**目标：多个入口复用执行规则，但执行层不理解 HTTP 和 SQL。**

拆两段：

```text
HTTP 层：解码、读取 Identity/pin/session 等传输输入
   ↓
executor：按原顺序完成解析、授权相关检查、默认值、目录准备、记录启动和执行参数
```

执行：

- [ ] 提取实际跨入口复用的参数，不传整个 `*http.Request`。
- [ ] 保留请求校验与 prepare 内各步骤相对顺序。
- [ ] 保留 request ID/started 的生成位置与日志启动时机。
- [ ] session seed 保持现有身份隔离、字段和算法。
- [ ] 模型聚合和默认设置以消费方接口提供，不 import SQLite。
- [ ] 分清 API 展示缓存、runtime 目录和 pool proven membership，不合并缓存。
- [ ] gateway/console 使用同一展示缓存服务，保持 TTL/key/refresh 去重。
- [ ] reasoning.go 保持 catalog-driven 和原 clamp 规则。
- [ ] 保留 CommitSession/ObserveStreamFailure 等完成反馈，先不发明通用 event stream。
- [ ] 三种协议和 console chat 都调用相同执行准备能力。

流式仍保持：

```text
handler decode → executor prepare/execute → handler relay SSE
                                         → 原时机反馈完成/失败和记录日志
```

**通过：** prepare 不依赖 HTTP/SQLite；四类入口执行轨迹一致，协议输出保持各自格式。

**回滚：** 可撤销某入口接入；不保留两份 prepare 业务规则长期分叉。

### S12：迁移 gateway

**目标：公有协议 HTTP 从 api 中独立。**

执行顺序：

1. OpenAI 非流式 handler。
2. OpenAI SSE。
3. Messages。
4. Responses。
5. `/v1/models`。

每个入口均执行：

- [ ] 创建明确依赖的 handler，不携带整个 Server。
- [ ] 移动已有解码、转换、输出和错误映射实现。
- [ ] 保留 headers/status/body/flush/defer 和日志收尾。
- [ ] 通过旧 routes 注入新 handler；路由包此时不必同时迁移。
- [ ] 测试继续从旧 URL 进入，而不只直接调用新包内部方法。
- [ ] 调用 executor 和共享查询接口，不直接访问 Store 或 Manager。
- [ ] 移除对应旧 handler 实现，或只保留单层无逻辑转发。

第一轮一个 gateway package，避免根包装配子包时又被子包 import 根包的循环。

**通过：** 三种协议兼容矩阵与 stream 测试通过，路由/鉴权不变。

**回滚：** 逐协议撤销接线；协议算法移动和路由注册分开审查。

### S13：迁移 console 与更新控制面

**目标：操作员 HTTP 不跑调度算法，不直接操作数据库和进程。**

建议顺序：

```text
logs/overview → providers/models → settings/keys
→ accounts/import/login → update → chat
```

执行：

- [ ] 每个 handler 只做 HTTP 输入、调用服务、响应映射。
- [ ] 保留账号子路径、导入、登录回调和手动刷新各 action。
- [ ] 保留 regional models 与普通 merged view 的差异。
- [ ] 保留 key secret 返回/脱敏、grants 和 last-used 更新语义。
- [ ] 保留 logs 过滤、分页、统计窗口、bucket 和 stats cache。
- [ ] 更新 handler 调用 update 协调能力，不自己再保存一份 maintenance/job。
- [ ] server 通过只读接口/函数读取同一 maintenance 状态；update 不依赖 server。
- [ ] 保留 prepare/apply/cancel/rollback 状态转移、任务互斥、agent 接管和超时。
- [ ] console chat 复用同一个 executor，不向自己的 `/v1` 发 HTTP 请求。
- [ ] 初期可复用同一个 OpenAI handler 实例作为两条路由入口，鉴权仍分别包装。
- [ ] 最终将 HTTP 协议复用与执行复用分清，不复制 SSE、prepare 或 failover。

**通过：** 前端无需改动；所有 console 路由仍要求管理员 key；更新验收独立通过。

**回滚：** 按功能组撤销，更新状态迁移单独提交，避免被普通查询接口变更捆绑。

### S14：提取 server/app，切换 cmd 接线

**目标：应用组装不再住在 HTTP Server 对象里。**

先 server：

- [ ] 迁移原 route 注册、health、中间件和 webui fallback。
- [ ] 接收构造好的 handler 和 auth/update 读接口，不自行建 Store/Manager。
- [ ] CORS → OPTIONS → maintenance → mux/auth 顺序按原实现保持。
- [ ] 保留 ServeMux 的精确/前缀匹配和默认错误行为。

再 app：

- [ ] 原样记录 New 内的每一步初始化以及后台 goroutine 启动点。
- [ ] 将 store/config bootstrap/log ring/provider/runtime/executor/control/handler 接线归位。
- [ ] 保留默认 data/runtime 目录、key/proxy bootstrap 优先级和错误行为。
- [ ] 保留 manager.Start、provider SetProviders、refresh/maintenance 的真实顺序。
- [ ] app 只持有需要关闭的资源和对外 Handler，不提供服务定位器给各模块。
- [ ] 保留 shutdown 顺序；不要擅自修改 panic/error、重入 Close 或 timeout 策略。
- [ ] 确认新 app/server/gateway/console 不再 import 旧 api。
- [ ] 此时才允许 `api.New → app.New` 兼容转发。
- [ ] 最后修改 `cmd/server` 的 import 与构造调用。
- [ ] cmd 的 dotenv、配置来源、信号、ReadHeaderTimeout/IdleTimeout/Shutdown timeout 不变。
- [ ] `cmd/updater` 不动。

**通过：** 从 cmd 构建和进程级 smoke test 通过；初始化轨迹与旧版一致；无循环 import。

**回滚：** 切换 cmd 的提交可单独撤销；兼容 api 门面存在期间可恢复旧调用名，但它不是两套 runtime。

### S15：清理、依赖守卫和最终验收

**目标：没有过渡垃圾，也不会回到大包模式。**

执行：

- [ ] 删除无调用的 alias、转发函数、旧字段、重复 helper 和废弃构造入口。
- [ ] `api` 保留还是删除单独决定；保留时只允许兼容门面，不加业务。
- [ ] 依赖检查临时 allowlist 清零，或每项明确原因与后续删除条件。
- [ ] 增加 import 约束 CI，覆盖生产包，测试例外单独管理。
- [ ] 更新 ARCHITECTURE 的职责图、依赖图、状态所有权和生命周期。
- [ ] 更新 AGENTS 的新边界与 migrations 新文件位置说明，保留 SQL 字节纪律。
- [ ] PLAN 记录每阶段实际验收结果和真实账号限制。
- [ ] 不给文档和注释中的旧路径留下误导性指引。
- [ ] 运行完整测试、vet、race、build 和隔离 smoke 验收。
- [ ] 完成发布与回滚演练，不自动发布。
- [ ] 记录尚未执行的真实账号/托管更新验证，不能标为全部完成。

**通过：** 第 13 节完成标准全部满足或明确由负责人接受剩余限制。

**回滚：** 清理提交先撤销；若涉及已删除门面，先恢复兼容层再撤销依赖它的变更。共享分支使用 revert，不改写历史。

---

## 9. 测试与差分验证方案

### 9.1 测试分层

| 层级 | 目的 | 方式 |
|---|---|---|
| 单元测试 | 分类、转换、权重、grants、seed、默认值 | 现有 table-driven tests + 补遗漏 |
| HTTP 契约 | 从真实路由保护状态码/body/header | `httptest` + fake 服务 + 临时库 |
| SSE 集成 | flush、取消、断流、Body 和事件顺序 | 真实测试 HTTP server + 可控 reader/writer |
| 数据兼容 | 老库启动、checksum、事务与恢复 | 合成数据库 fixture + 迁移摘要 |
| 生命周期 | Start/Stop、重启、维护任务和关闭 | fake starter/clock 或已有时间注入点 |
| 差分测试 | 旧新执行轨迹一致 | 同一 fixture 分别喂给隔离旧/新实现 |
| 真实验收 | 云端协议与环境交互 | 显式 opt-in、隔离测试账号 |

不要为了测试时间而一次替换全系统 clock。优先使用已有注入点；确需时间 seam，独立提交且默认实现仍是原调用。

### 9.2 核心用例矩阵

以下是最低覆盖方向，实际先映射已有测试再补齐。

| ID | 场景 | 关键断言 |
|---|---|---|
| H01 | health、公共路由、console 无 key | 开放/拒绝范围、错误体不变 |
| H02 | console key / client key / provider-region grants | 权限与返回码不变 |
| H03 | OPTIONS + Origin + maintenance | CORS 和执行顺序不变 |
| H04 | 不支持 method、未知路径、尾斜杠 | ServeMux 和 handler 原行为 |
| H05 | 静态资源和 SPA 深链 | 允许列表、不扩大 fallback |
| P01 | OpenAI/Messages/Responses 非流式 | 文本、tools、reasoning、usage、finish reason |
| P02 | 三协议流式 + console chat | SSE 事件与终止语义 |
| P03 | malformed JSON、空消息、上下文限制 | 原错误优先级与 body |
| P04 | reasoning default/client/catalog clamp | onlyReasoning、合法更高 effort 不受默认值限制 |
| P05 | bare/provider-prefixed model、cross-provider 开关 | 模型解析与 pool 限制 |
| R01 | 显式 pin、sticky、普通 pool | 选择轨迹与优先级 |
| R02 | 无 user anchor、image-only、后续轮次 | seed 稳定性与退化路由 |
| R03 | provider/region/grants 不匹配 | 不跨边界逃逸 |
| R04 | 三种 routing strategy | 权重、饱和、就绪/冷却选择序列 |
| R05 | quota/rate_limit/auth/not_ready 等分类 | HTTP、failover、Retry-After、cooldown |
| R06 | model-scoped cooldown、catalog/proven | nil/empty、模型互不误伤、成功证明保留 |
| R07 | 并发状态快照乱序到达 | StateVersion 防止旧状态覆盖新状态 |
| S01 | 正常 SSE、工具、reasoning、usage | 事件内容/顺序、usage 与日志 |
| S02 | HTTP 错误发生在流开始前 | 原重试与返回方式 |
| S03 | 错误事件、截断 EOF、缺少终止标记 | 原结束/失败判定 |
| S04 | 客户端取消、写失败 | ctx 传播、Body 关闭、in-flight 释放 |
| S05 | 流开始后失败 | 不额外重放、绑定与日志完成时机 |
| D01 | 空库、当前库、旧版合成库 | 启动、迁移、读写与重启 |
| D02 | 历史 migration 内容 | filename/order/SQL 摘要完全一致 |
| D03 | 凭据导入导出、key、secret | 原格式、脱敏、权限与错误 |
| D04 | SQLite backup/restore | 一致性校验、WAL 场景、保留策略 |
| L01 | 账号新增/修改/删除/导入失败 | 写库/runtime/pool 的原副作用顺序 |
| L02 | 子进程退出、启动失败、并发删除 | 重启次数、退避和无重复任务 |
| L03 | probe/quota/catalog 刷新 | quota 错误不影响 Ready，刷新去重 |
| L04 | 签到/保活 opt-in、本地日界和 retry | 原执行时间与状态写入 |
| U01 | prepare/apply/cancel/rollback | 状态转移、互斥和 maintenance |
| U02 | agent 状态接管、失败/取消/重启 | 原任务恢复和解除阻断规则 |
| O01 | request/attempt/stream finalization | 记录数量、耗时起点、usage 与错误口径 |
| O02 | logs/stats/overview | 过滤、分页、bucket、cache 与 refresh |

### 9.3 Golden 与差分测试规则

可以归一化：

- 随机 request/job ID 的具体值；
- 非契约性的时间戳值，但保留格式与相对顺序；
- 测试分配的临时端口/目录。

不能归一化掉：

- 状态码、错误 code、业务错误字段；
- JSON 字段缺失、null、空集合差异；
- SSE 事件内容与顺序；
- account/provider/region 选择结果；
- retry 次数、cooldown 类别、日志是否创建或完成。

普通 JSON 可结构化比较，同时保留字段存在性；SSE 不把实际协议帧全部压平成“最后文本一样”。

差分测试记录的轨迹建议包括：

```text
校验结果 → 目录刷新 → 选号 → 上游调用 → attempt
→ 流提交/转发 → 绑定/冷却 → 最终记录 → 资源释放
```

- 相同 fake 输入分别执行旧版与新版，使用不同临时 DB/HOME/端口。
- 不让旧新进程共享同一个 Qoder HOME 或真实账号 worker。
- 不在生产将请求同时发给两版上游；这会造成双扣费、会话分叉和重复副作用。
- Golden 更新必须单独审查。纯重构不应该通过批量更新快照改变预期。

### 9.4 Migration 摘要测试实现要求

建议以现有 migration 清单为输入，固定每条已发布项的：

```text
filename + 顺序 + 原始 SQL 内容 hash
```

优先使用包内测试访问真实 migration 集合，不用正则重新解释 Go raw string。移动文件后测试跟随集合归属迁移；不要增加只为测试服务的生产导出 API。

检查两类数据：

1. 迁移前记录的固定摘要与迁移后内容一致；
2. 用旧 schema/migration 记录构建的合成数据库能被新程序正常打开。

新库启动成功不能替代旧库兼容测试。对生产真实数据库只做授权、受控的备份/验收，不纳入仓库 fixture。

### 9.5 验证命令

常规 Go PR：

```bash
go test ./...
go vet ./...
go build ./cmd/server ./cmd/updater
git diff --check
```

涉及并发与生命周期：

```bash
go test -race ./...
```

执行前确认平台支持 race；环境失败与逻辑失败分别记录，不伪装成通过。

worker：

```bash
(cd worker && npm test)
```

前端基线/最终验收：

```bash
(cd frontend && npm ci && npm run build && npm run lint)
```

仅在 UI 确实发生变更时按仓库要求执行：

```bash
(cd frontend && npm run sync)
```

本轮不计划改 UI，因此不应出现新的 embed hashed JS/CSS 差异。如出现，先解释来源，不自动提交。

补充 import 图检查：

```bash
go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/...
```

定向测试可根据当前阶段实际包位置执行，但不能代替合入前全量 Go 验证。

---

## 10. PR、评审与持续集成

### 10.1 建议 PR 切分

| PR | 范围 | 不应混入 |
|---|---|---|
| 01 | 基线、契约测试、规则说明 | 业务实现迁移 |
| 02 | api 同包拆文件 | handler 语义调整 |
| 03 | accounts 同包拆文件 | 锁/进程策略修改 |
| 04a… | 类型与消费方接口 | 大规模命名统一 |
| 05 | Store 和 migration 归属 | SQL/schema 变化 |
| 06a… | control 操作逐项迁移 | CRUD 异步化/补偿机制 |
| 07a… | runtime 生命周期迁移 | 启动顺序优化 |
| 08 | Qoder 协议/进程具体实现归位 | CLI 升级 |
| 09a… | Qoder Adapter 分能力接线 | provider 行为统一 |
| 10 | Pool/route/classify 归位 | 算法改进 |
| 11a… | 共享 prepare/模型服务 | 缓存合并/刷新策略改变 |
| 12a… | gateway 分协议迁移 | 新协议功能 |
| 13a… | console 分功能迁移，update 单独 | 前端改版 |
| 14a… | server、app、cmd 接线 | 超时和错误处理优化 |
| 15 | 删除过渡层、依赖 CI、文档 | 顺手修复其他问题 |

阶段不等于一个 PR。风险高的阶段拆多个，保持一个 PR 只让评审者理解一个主要边界变化。

### 10.2 单个 PR 的固定施工流程

1. 同步已验收代码，检查工作区。
2. 说明来源、目标、受影响入口、共享状态。
3. 先运行相关现有测试，确认基线。
4. 做最小移动；必要 import/可见性调整与行为调整分开。
5. 编译并运行定向测试。
6. 检查 diff 是否出现 SQL、超时、条件顺序、JSON tag、goroutine 的意外变化。
7. 运行全量验证。
8. 填写验收/未验证项、回滚说明。
9. 评审通过后合入，更新 PLAN 的实际结果。
10. 下一阶段从该节点继续，不能带着失败“等全部搬完再修”。

### 10.3 评审重点

- 新对象是否复制了原 Server/Manager 的全部依赖？如果是，只是改名的大对象。
- 相同缓存、pool、affinity、maintenance 是否出现两个实例？
- helper 导出是否只是为了绕过错误包边界？
- 新接口是否真的有独立消费方？
- defer、context、锁范围、channel close 和 goroutine 数量是否变化？
- 是否有 `errors.New(err.Error())` 等破坏错误身份的包装？
- 是否丢失 optional 接口、Flusher、取消传播或 Body 所有权？
- 是否将 nil slice/map 变成空集合，影响 routing 或响应字段？
- 是否把 SQL 事务拆成多次 service 调用，暴露中间状态？
- 是否只更新了快照，让“兼容测试”接受了新行为？

### 10.4 依赖守卫如何落地

使用 Go 实际 import 信息或 Go AST 检查，不依赖字符串 grep 猜测 import。

最小规则：

- gateway/console 不 import store、`database/sql` 或 SQLite 驱动。
- providers 子包不 import executor、runtime manager、gateway、console、store。
- executor 不 import具体 provider、store 或 handler 包。
- store 不 import control、runtime manager、app、handler。
- logs 不 import具体 store。
- app 没有被底层包引用。
- 最终旧 api 只剩允许的兼容门面。

可以在现有 CI 使用一个专门的 Go 架构测试或小脚本；不需要新增工程框架。测试代码为集成测试使用 app/store 是合理的，应与生产 import 分开检查。

迁移期间 allowlist 必须写明：旧依赖、保留原因、删除阶段。新增例外需评审，禁止无限扩张。

---

## 11. 发布、观测与回滚

### 11.1 不默认发布每个结构 PR

每个 PR 都应可构建和回滚，但不要求每个移动文件提交立刻上生产。是否阶段发布由负责人决定。

正式发布仍执行 `DEVELOPMENT.md` 的现有 workflow，不手动打 tag，不绕过 main 保护，不因 worktree 占用强行 checkout main。

### 11.2 发布前

- [ ] 全量 Go/worker 验证，必要 race 和前端基线验证通过。
- [ ] 迁移摘要一致，确认无新 schema/数据格式。
- [ ] 明确当前部署版本和待发布版本。
- [ ] 使用现有 SQLite 一致性备份方式，验证备份可读。
- [ ] 确认备份包含敏感数据的处理权限，不复制进仓库。
- [ ] 使用独立测试 data dir、HOME、端口启动候选版本。
- [ ] fake 环境验证启动、登录回调、请求、取消、停机、重启。
- [ ] 真实 provider 验收使用授权测试账号，确认配额和副作用。
- [ ] 托管更新流程在隔离环境验证，不能直接拿生产 apply 做 smoke test。
- [ ] 明确回滚操作人、触发条件和允许的数据损失范围。

### 11.3 观测项

不为本轮额外引入观测平台，优先利用现有 logs/stats/runtime 状态：

- HTTP 错误分布、协议错误 code；
- 首 token/TTFB、流失败与取消；
- attempts、failover、sticky 命中/逃逸；
- 冷却账号和模型数量、恢复情况；
- worker 数量、restart 次数和探测失败；
- 请求日志缺行、重复完成、统计异常；
- maintenance 是否按任务结束解除；
- 数据库错误和 migration checksum。

阈值根据发布前基线约定，不在本文凭空指定一个延迟/错误率数字。没有历史负载时先用同一 fake workload 对比，再小范围真实验收。

### 11.4 回滚原则

**代码回滚和数据恢复是两件事。**

1. 先停止继续发布或执行新更新任务，保留必要的脱敏诊断信息。
2. 按现有部署/更新流程恢复到已验收版本。
3. 本轮没有 schema 变化时，优先使用当前兼容数据库启动旧版，不盲目恢复旧备份。
4. 若确实发生数据损坏，按授权 runbook 恢复已验证备份；明确备份后的账号、设置、日志可能丢失。
5. 不让旧版和新版同时以相同 HOME 管理同一账号 worker。
6. 验证 health、auth、账号数、worker 数、models 和一条受控请求。
7. 记录失败阶段和根因，回到最近通过的门槛，不继续向后搬。

回滚共享 Git 分支用 revert；不强推改写历史。若多个后续 PR 依赖某次边界调整，按依赖逆序回滚，或做最小修复，不能只撤中间提交让构建断裂。

---

## 12. 风险与暂停条件

| 风险 | 典型信号 | 处理 |
|---|---|---|
| 包名变化掩盖职责未变 | 新 handler 仍拿全量 Store/Manager | 缩回一步，先提消费方接口 |
| 循环依赖 | accounts↔store、provider↔executor、api↔app | 调整类型/装配方向，不用全局变量绕过 |
| 状态复制 | 两个 Pool/LRU/cache/update job | 暂停接线，恢复唯一所有者 |
| migration 损坏 | 摘要改变或旧库 checksum mismatch | 阻止合入/发布，恢复历史 SQL |
| 校验顺序变化 | 相同错误输入返回不同 code | 还原顺序，不更新 golden 接受变化 |
| SSE 回归 | 无 flush、挂起、重复日志、取消不退出 | 缩小到单个 relay 边界重新验证 |
| 调度语义变化 | 轨迹不同、跨 region、模型互相冷却 | 保留旧算法，检查数据转换和状态实例 |
| 生命周期重复 | 双 worker、重复 checkin/restart | 检查构造和 Start 所有权，停止后续迁移 |
| optional 能力丢失 | 编译通过但代理/设置不再生效 | 检查 wrapper 的 type assertion 能力 |
| 更新死锁/阻断不解除 | maintenance 与 job 分属两个对象 | 用同一协调状态，独立回归 update |
| 测试“全部通过”但无实际保护 | 测试只调用新 helper，不经过路由 | 补入口级契约测试 |
| 真实环境不明确 | 默认测试请求云端或启动生产 HOME | 停止执行，隔离测试资源 |

以下任一情况出现，必须暂停当前阶段：

- 需要修改历史 SQL、worker 协议或前端才能“让重构跑起来”。
- 无法解释新增 goroutine、锁变动、超时或缓存 TTL 的原因。
- 需要同时改多个产品协议才能完成一个目录移动。
- 既有测试失败只能通过删断言或批量更新快照解决。
- 无法说明状态由谁创建、谁关闭、谁能修改。
- 一个 PR 无法在不理解多个独立业务系统的情况下审查。

暂停不是失败。应将行为修复/新能力单独拆出，或缩小边界迁移，再继续。

---

## 13. 完成定义与后续功能定位

### 13.1 结构完成

- [ ] app 只接线和管理应用生命周期。
- [ ] server 只注册原路由、middleware 和静态入口。
- [ ] gateway/console 不直接依赖 SQLite 或子进程实现。
- [ ] 公有协议与 console chat 共用一个执行规则来源。
- [ ] control 不拥有调度算法、SQL 或 Qoder 进程参数。
- [ ] runtime 不写产品协议，不反向依赖 control。
- [ ] Qoder 的具体实现与其他 provider 一样有明确归属。
- [ ] accounts 不再聚合 Store、Manager、Pool 的全部实现。
- [ ] store 只拥有持久化实现和必要的存储逻辑。
- [ ] logs 不依赖具体 store。
- [ ] 每个关键状态只有一个实际所有者。
- [ ] 无长期重复执行路径、临时 alias 和无理由 allowlist。

### 13.2 兼容完成

- [ ] HTTP、鉴权、CORS、maintenance、静态资源契约通过。
- [ ] 三种协议、console chat、SSE 成功/失败/取消通过。
- [ ] 模型、reasoning、grants、routing、affinity、cooldown 通过。
- [ ] 旧库启动、SQL 摘要、凭据、导入导出和备份通过。
- [ ] runtime 恢复、刷新、签到保活、更新生命周期通过。
- [ ] 前端不需要适配，worker pinned 资产不变。
- [ ] 真实账号与托管更新验收已执行，或未执行限制被明确记录和接受。
- [ ] 发布回滚方式可执行，不只写“git revert 即可”。

### 13.3 工程完成

- [ ] CI 守住依赖方向。
- [ ] 文档与真实目录一致。
- [ ] 新功能不需先理解整个 Server 或 Manager。
- [ ] 核心测试跟随职责存在，同时保留入口级契约保护。
- [ ] 没有为了目录美观引入更多抽象和维护负担。

### 13.4 以后改哪里

| 需求 | 首选位置 | 通常还要验证 |
|---|---|---|
| 新客户端协议或协议字段 | gateway + translate | 原协议回归、SSE、执行准备 |
| 修改请求校验或公共默认规则 | translate / executor.prepare | 错误优先级、reasoning、各入口 |
| 新 provider | providers/<name> + app 注册 | 账号、权限、目录、执行、runtime 能力；先过里程碑审批 |
| 修改 Qoder 登录/worker 协议 | providers/qoder | region、鉴权、HOME、兼容 hooks |
| 修改登录回调 HTTP | console/accounts | provider 登录能力与错误映射 |
| 修改账号 CRUD/import 流程 | control/accounts | 数据库/runtime/pool 副作用顺序 |
| 修改重启/探测/维护时间 | runtime | 并发、关闭、时区、状态持久化 |
| 修改选号/重试/冷却/粘性 | executor | REQUEST 全契约、选择轨迹、stream 反馈 |
| 修改模型目录展示 | 共享模型服务 + gateway/console 编码 | regional/merged、cache、grants |
| 修改模型默认设置 | control/models + store | executor 默认应用、catalog reasoning |
| 修改密钥/权限 | auth + control/keys | console/client、provider/region grants |
| 修改请求记录/统计 | logs + store 对应查询 | attempt、SSE 收尾、分页和 bucket |
| 修改表结构 | store 新 migration | 旧库升级与回滚；绝不改历史 SQL |
| 修改路由/CORS/maintenance 包装 | server | auth 顺序、OPTIONS、静态 fallback |
| 修改托管更新控制面 | update + console/update | 状态机、maintenance、agent 兼容 |
| 修改托管更新 agent | updater + cmd/updater | 单独工作，不作为本轮附带清理 |
| 修改应用启动/关闭接线 | app + cmd/server | 配置优先级、资源唯一性和生命周期 |
| 修改 UI | frontend | DESIGN、原 API 契约、npm run sync |

是否抽新包的判断：它是否有独立职责、依赖、状态或测试边界？仅仅因为文件超过某个行数，不足以创建一个新 package。

---

## 14. 执行记录模板

### 14.1 阶段开始前

```text
阶段编号：
当前状态（未开始/进行中/待验证/阻塞）：
对应复选框位置：
起点 SHA：
目标职责：
来源文件/符号：
目标文件/包：
涉及入口：
涉及共享状态及当前所有者：
允许修改：
明确不改：
前置测试结果：
循环依赖检查结果：
需要人工确认的副作用：
```

### 14.2 PR 描述

```text
本次只做：

来源 → 目标：

保持不变的契约：
- 路由/鉴权：
- 请求/流式/调度：
- 数据与文件：
- 生命周期：

状态所有权是否变化：
调用、锁、事务、defer 顺序是否变化：
新增接口及其真实消费方：
临时兼容层与删除阶段：

已运行：
- 命令：
- 结果：

未运行：
- 原因：
- 风险：

Migration 摘要比较：
回滚方法：
是否需要真实账号/隔离更新验收：
```

### 14.3 阶段验收

```text
阶段编号：
验收日期与确认人：
本次勾选的任务：
未勾选任务、例外批准与影响：
阶段复选框是否允许勾选：
合入/候选 SHA：
完成的职责迁移：
保留的临时依赖：
测试证据：
真实环境验收证据（脱敏）：
未覆盖项与接受人：
是否允许进入下一阶段：
失败时回退到哪个已验收节点：
```

### 14.4 给执行者的最终指令

> 每次只执行当前获准阶段。先读取现行约束和上一阶段验收记录，确认工作区与最新 main，再运行相关基线测试。只移动一个明确的职责边界，不改变算法、响应、SQL、配置、超时和生命周期语义。遇到新行为需求、循环依赖、共享状态复制或无法解释的测试差异，停止并报告。完成每项任务并验证后及时勾选本文对应复选框；停止工作前更新进行中、待验证和阻塞项；阶段验收通过后再勾选阶段摘要，并同步 PLAN。完成后列出实际修改、已运行与未运行的验证、剩余兼容层及回滚方式，不自动进入下一阶段，不自动部署。

**最终衡量标准不是目录数量，而是：下一次修改有明确归属、明确影响范围、明确测试和回滚方式。**
