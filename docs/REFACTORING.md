---
id: cli2api-behavior-preserving-refactoring
title: 已验收的后端包边界
scope: [backend, package-boundaries, regression-tests]
status: accepted
read-when: 查现行包归属、剩余 import allowlist、职责残留处理方案、或未宣称的真实账号限制时
summary: S00–S15 包图与 A–D 职责收口已验收；记录现行边界、例外与回归结果。
related: [AGENTS.md, docs/ARCHITECTURE_SUMMARY.md, docs/DEVELOPMENT.md]
last-updated: 2026-09-19
---

# 已验收的后端包边界

S00–S15 行为保持拆分已完成。不要再按旧 `internal/api` 大包或本文历史阶段去改代码。现行硬规则在 [`AGENTS.md`](../AGENTS.md)，公共运行时摘要在 [`ARCHITECTURE_SUMMARY.md`](ARCHITECTURE_SUMMARY.md)；更详细的本地契约不在干净 checkout 中保证存在。

核对其他文档时，以本文的现行包图和已完成记录为基准；行为细节仍需对照当前代码。文末 A–D 是已完成的历史收口记录，不是待办清单，也不是重新执行 S00–S15。

## 现行包图

```text
cmd/server → app.New
               ├─ server     路由、CORS、OPTIONS、maintenance、webui
               ├─ gateway    /v1/chat/completions /v1/messages /v1/responses /v1/models
               ├─ console    /api/* 操作员 HTTP；/api/chat 复用同一 gateway 执行路径
               ├─ update     Coordinator：job / maintenance
               ├─ executor   Pool / route / classify / prepare / failover
               ├─ control    账号/密钥/设置/备份/登录/导入编排；目录聚合与展示缓存
               ├─ runtime    Manager 生命周期、进程表、刷新、签到循环
               ├─ store      SQLite；migrations.go SQL 字节不可改
               ├─ providers  qoder / workbuddy / trae / devin
               └─ logs/auth/endpoint/translate/proxy
internal/api   测试兼容门面：api.New → app.New。生产 cmd 不走这里，不加业务。
```

| 改什么 | 去哪 |
|---|---|
| 公有协议 / SSE | `internal/gateway` + `translate` |
| 控制台 HTTP | `internal/console` |
| 路由 / CORS / webui | `internal/server` |
| 启动关闭 / 注入 | `internal/app` + `cmd/server` |
| 选号 / 冷却 / prepare | `internal/executor` |
| 账号 CRUD / keys / settings / 登录编排 / 目录聚合 | `internal/control` |
| 子进程启停 / 恢复 | `internal/runtime` |
| Qoder HOME / worker 协议 | `internal/providers/qoder` |
| SQLite / 新表 | `internal/store` 新编号 migration |

## 剩余生产 import allowlist

守卫测试：`go test ./internal/app -run 'TestImportConstraints|TestDutyBoundaries'`。CI 先跑 import 守卫。测试 import 不计入。

| 边 | 原因 | 删除条件 |
|---|---|---|
| `api` → `app` | 测试门面 | 测试全部迁出 `package api` 后可删门面 |
| `executor` → `qoder` | chat 仍走 worker HTTP `qoder.NewChatRequest` | 生产 chat 切到 Adapter |
| `runtime` → `qoder` | spawn / HOME / quota / catalog 仍直接调 qoder | 剩余能力切到 Adapter |

## 未宣称完成

- 真实账号：Qoder CN L6、WorkBuddy、Trae T5
- 托管更新 apply；正式发布
- Restore API；pin 缺失仍回退 pool；Delete 不立刻清 `recovering[]`
- Qoder quota / login / chat 生产路径仍走 worker HTTP；WorkBuddy / Trae / Devin 走进程内 adapter

回滚：无 schema 变化时，用同一 SQLite 启动上一验收二进制；共享分支用 revert，不改写历史。


## 2026-09-19 审核问题修复与回归

基于 `b5c2b06` 修复；开工已 fetch/merge 最新 `origin/main`（Already up to date）。以下勾选只代表本地实现与验证，不代表真实账号或正式部署验收。

- [x] **鉴权/worker key 共享状态。** `auth.Verifier` 的副本共享原子密钥；HTTP 鉴权、console 展示、Qoder admin/catalog 转发与 executor 的 `WorkerKeySource` 读取同一 live 值。轮换不再替换 verifier 或改写 executor 副本；`Cfg.ProxyAPIKey`/`Executor.WorkerKey` 仅作 bootstrap 快照/独立构造 fallback，不作为生产 live 状态。轮换操作串行执行，runtime 的 key 读取使用已有锁。
- [x] **更新依赖仅装配一次。** 删除 `App.SyncUpdate`、`UpdateForRequest` 和重复的 App checker/agent 字段。HTTP 与后台任务共用已注入的 Coordinator；测试只在开始请求前给 Coordinator 注入 fake，不在请求期间热替换依赖。
- [x] **ExecStarter 并发安全。** 外层配置、懒初始化及 setter 使用同一把锁；快照不再回写共享配置，并保留完整 ManagerConfig 字段。进程 Start 在释放外层锁后调用内层已同步的 Starter。
- [x] **永久回归测试。** `internal/app/regression_test.go` 固定一次获取的生产 handler，验证连续两次轮换、旧 key 401/新 key 200、named key 权限不变，以及四个聊天入口 × 流式/非流式的真实 fake-worker HTTP 鉴权；另测轮换并发读取和 160 次并发更新查询。auth 测副本共享/并发轮换；runtime 测构造/字面量两种 starter 的 key、proxy、snapshot 并发。
- [x] **测试自身的竞态/时序修正。** 持久化失败测试改用消费方接口注入可恢复写入失败，不再并发替换 SQLite handle，继续断言 dirty 保留、版本不推进、恢复后落库；更新测试用可阻塞 staged agent 验证 maintenance/冲突/失败解除，而不是依赖瞬时 unsupported 错误；模型 fake worker 单独处理后台 health/quota 探测，未知路径仍报错。
- [x] **全量 race 门禁。** `.github/workflows/ci.yml` 增加 `go test -race ./...`；本地 `go test -race ./... -count=1` 已通过。

此前将四项 race 一律标记为“既有”不准确：ExecStarter wrapper 与 SyncUpdate 的问题由本轮重构引入；DB handle 替换是重构前已有的测试竞态。上述失败不再作为忽略 race 的理由。

验证证据（2026-09-19）：

- [x] `go test ./... -count=1`：通过。
- [x] `go test -race ./... -count=1`：通过，无 race 报告。
- [x] `go test -race ./internal/app ./internal/auth ./internal/runtime ./internal/api -run 'TestConsoleKeyRotation|TestConcurrentUpdateInfo|TestVerifierCopies|TestVerifierConcurrent|TestExecStarter.*Concurrent|TestPersistFailureKeepsDirtyEntryAndRetries|TestMaintenanceApplyConflictAndFailedAgentUnblock|TestSystemUpdateDoesNotBackupWhenNoNextVersionExists' -count=5`：通过。
- [x] `go vet ./...`、`go build ./cmd/server ./cmd/updater`：通过。
- [x] `staticcheck@2026.1 -checks=U1000 ./...`、`deadcode@v0.41.0 -test ./...`：无未使用或不可达函数报告。
- [x] worker `npm test`：55/55。
- [x] `python3 scripts/release-notes.py validate`、`git diff --check`：通过。
- [ ] 真实账号、真实托管更新 apply/发布：未执行，本次不声称通过。

不改 SQLite/migration 字节，不改 frontend、worker 运行协议，不自动提交、发布或部署。

## 2026-09-19 重构残留清理

在上述未提交修复基础上清理；开工 fetch/merge `origin/main`，结果为 Already up to date。不改变公开 HTTP 接口、Qoder worker 生产路径或 SQLite migration。

- 清理模型路由注册表、gateway 包装/Pool 注入和鉴权/代理/control 入口；这些符号没有生产调用链，也没有被测试兼容门面使用。
- 删除 Devin 未使用的目录注入状态、只写的 cacheSource、旧 metadata/session reset 包装。删除 WorkBuddy 旧 catalogPath；保留断言不请求旧目录接口的测试。
- 删除旧 `logs.PrefixWriter`，将账号前缀/跨写入分行测试迁到生产 `qoder.prefixLogWriter`；Devin 工具测试覆盖当前 `coreLocalTools`，仅用于断言的计数器迁入 `_test.go`。
- Qoder payload 统一调用 `qoder.BuildChatPayload`；模型设置 key/default 统一归 `control`（空 key 仍为空，不改成 routing 的 auto）；Classified → provider error 统一归 executor。App New/RebuildHTTP 共用 HTTP server 装配，目录 cache 去掉纯转发回调。
- 删除前端未引用的 `fetchOverview` / `rewarmWorker`，保留对应后端路由。使用 Node 22 执行 `npm run sync`，dist/static 一致；这两个函数原已被 tree-shaking，产物 hash 无变化。
- 补充模型 key/默认窗口、Retry-After fallback、payload token 优先级/省略字段/显式 false 回归。日志筛选测试改用唯一固定前缀，消除随机 request ID 前缀碰撞；该测试连续 20 次通过。删除 grants 测试的重复条件。

验证：`go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`、server/updater build、import 守卫通过；worker 55/55；frontend sync/build/lint 通过（既有 lint 与 chunk size 警告仍在）。

清理前静态检查发现的残留已删除；清理后重新运行 `deadcode@v0.41.0 -test ./...` 与 `staticcheck@2026.1 -checks=U1000 ./...`，结果记录在本节验证项中。扫描无报告不代表不存在条件分支或动态调用层面的残留。

刻意保留：api 测试门面、RebuildHTTP 等测试支持入口、Qoder 尚未接管生产的 Adapter 能力、各 HTTP/provider 包内小型响应/协议骨架重复。后续若收敛这些内容，应迁移测试或按协议契约单独验证，不以 deadcode 结果机械删除。


本轮还修正了 WorkBuddy/Trae 注释中对旧 `api.modelContextKey` 的引用，并把包职责文档同步到当前 `store`、`runtime`、`executor`、`control` 的实际归属。生产 `/v1/*` 仍通过 `server → gateway → executor`，没有改请求契约、worker 协议或 SQLite migration。

目录聚合、系统设置 PATCH、控制台密钥轮换、凭据导入和 in-process 登录编排已下沉到 `control`；adapter 错误分类与本地午夜冷却在 `executor`。OAuth loopback 的 `http.ResponseWriter` 只留在 `auth.ServeLoopback`。守卫：`go test ./internal/app -run 'TestImportConstraints|TestDutyBoundaries'`。

仍保留的装配债务：`internal/app/workerproxy.go` 仍为 `control.CatalogSource` 注入 Qoder worker 目录函数和 pool 账号快照。app 只应装配数据源，不能把聚合/权限过滤/设置装饰再实现一遍，也不能让 `control` 反向依赖 runtime 或具体 provider。Qoder 登录/quota/chat 生产路径仍走 worker HTTP。

## 2026-09-19 职责越界清理

在上述残留清理基础上继续下沉，不改变公开 HTTP 接口、Qoder worker 生产路径或 SQLite migration。

- Trae/Devin 不再设置 `Failover` 或冷却时长；adapter 只返回 Kind/Status/Message/Code，以及 WorkBuddy 从上游解析出的 Retry-After。Trae `4011` 的 5 分钟硬限流由 `executor.ClassifyError` 按错误码决定。切号与冷却统一归 executor，quota 不再被 provider 覆盖成可切号。
- 控制台模型设置 PATCH 只解码；WorkBuddy 禁止 Max、读旧值合并写回收进 `control.Settings.UpdateProviderModelSetting`。
- 账号登录/导出/签到分派收进 `control.Accounts.Admin` / `Export`。console HTTP 校验方法并解码；Qoder worker 路径与 `oauth_if_complete` 由 `qoder.AdminAction` 封装，runtime `WorkerAdmin` 按动作名转发，不改 worker 路径。
- 账号创建、凭据导入回滚、启用顺序收进 `control.Accounts`。runtime 只保留 `StartAccount` / `StopAccount` / `SyncAccount` / `RemoveAccount` 与进程视图；测试仍可走 Manager 的 Create/Update/Delete 包装。
- 账号默认并发/优先级、签到时间继承、按 provider 的代理限制收进 `accounts` 领域函数。store 创建/更新调用这些规则，不再内嵌策略。
- 日志统计时间窗与 10 秒缓存收进 `logs.RequestRecorder.Stats`。console 只解析查询参数。

守卫：`go test ./internal/app -run 'TestImportConstraints|TestDutyBoundaries'`。

## 2026-09-19 职责残留处理方案

### 先说结论

不用推倒重构，也不用再拆一批包。把剩下的业务决定放回正确的包，现有 HTTP 接口和数据库结构尽量不动。

可以这样理解各层：

| 层 | 应该做什么 | 不应该替谁做决定 |
|---|---|---|
| provider | 告诉系统上游发生了什么、协议怎么调用 | 不决定切号和账号冷却 |
| executor | 决定选谁、失败后怎么办、何时重试 | 不把自己的决定伪装成上游原始错误 |
| gateway / console | 读取 HTTP 请求，输出 HTTP / SSE 响应 | 不重新决定重试、设置规则或密钥业务流程 |
| control | 编排账号、设置、密钥等操作 | 不实现 SQLite 或具体 worker 协议 |
| accounts | 提供可复用的领域类型和纯规则 | 不依赖 runtime、executor、store |
| store | 保存、读取数据，维护事务和约束 | 不成为业务规则和操作流程的唯一实现 |
| runtime | 管进程、运行状态、等待和凭据同步 | 不靠具体登录 URL 猜测协议要求 |

这里需要区分两件事：**没有违规 import，不等于所有职责都已经放对；但 store 生成记录 ID、执行事务或调用领域校验，也不自动构成越界。** 本次第 3、4 项是职责收拢，不是宣称所有持久化校验都必须删除；第 5 项是 Qoder 白名单路径内部的协议残留。

### 改动顺序与范围

| 步骤 | 对应复查问题 | 具体交付 | 为什么这样排 |
|---|---|---|---|
| A | 1. 错误类型混用；2. gateway 指定切号 | executor 拥有最终错误，HTTP 层只格式化 | 两项共用错误传递链，先一起修，避免改一半 |
| B | 3. 模型设置规则分散 | control.Settings 提供完整读写操作 | 去掉 console 与 store 两端的业务分叉 |
| C | 4. 普通 API Key 流程在 store | control.Keys 编排，accounts 提供规则，store 写数据 | 与控制台主密钥轮换分开，不扩大风险 |
| D | 5. runtime 判断 Qoder 登录路径 | qoder 声明动作要求，runtime 按声明协调 | 最后做小范围协议收口，不切换生产 Adapter |

默认按 A → B → C → D 实施。每一步独立完成测试、审查和文档记录后，再进入下一步；B、C、D 不应成为 A 修复的前置条件。下文类型与方法名是建议命名，不要求照名字新增框架。

### A. 错误只分类一次，决策不在传递过程中变样

**现在的问题**

入口：`internal/executor/chat.go` 的 `pick`、`coolingPickError`、`ProviderErrorFromClassified`；`internal/executor/error.go` 的 `ClassifyError`；`internal/gateway/openai_stream.go` 的流错误处理。

```text
现在：executor 决定重试 5 秒
        → 包装成 providers.Error
        → gateway 调用 ClassifyError，又当成上游限流
        → 对外变成 30 秒

目标：provider 原始错误 → executor 分类 → executor 的最终错误 → HTTP / SSE 展示
      executor 自己产生的调度错误 ────────┘                    不再重分类
```

上面的 5 → 30 秒来自当前源码调用链：全部账号并发占满时 `pick` 设置 5 秒，而 `ClassifyError` 对 provider 限流施加 30 秒下限。实施第一步要补穿过 HTTP 输出层的回归测试，而不只检查 executor 返回了 429。

**具体怎么改**

1. 在 `internal/executor/error.go` 增加 executor 自有错误类型，例如 `ExecutionError`，保存完整 `Classified` 和可选原始原因；提供 `Error` / `Unwrap`，保留 `errors.Is/As` 的取消识别能力。不要新建公共大杂烩 errors 包。
2. `ClassifyError` 先识别这种已分类错误，原样返回其分类；只有原始 provider / transport 错误才进入分类规则。`Status`、`Code`、`Type`、`Kind`、`Failover`、`Cooldown`、`RetryAfter`、`Model` 都不能在转换中丢失或重算。
3. 修改选号失败、冷却中、失败重试结束等出口，让 executor 返回自己的错误。逐一迁移 `ProviderErrorFromClassified`、`providerErrorFor` 及对应 `errors.As(*providers.Error)` 调用者；不能只新增类型而保留旧生产出口。
4. gateway 保留 SSE 拆帧、错误体提取和客户端断连识别，但把“上游流提前结束／读失败”等事实交给 executor 的流故障入口。gateway 不再传 `failoverHint="1"`，也不自己指定冷却策略。executor 决定结果，gateway 负责三种公开协议的输出。
5. 同步更新 `ObserveStreamFailure`、请求日志和三个协议的错误输出，让它们消费同一个分类结果。客户端断连仍记为取消，不能顺手给账号加冷却；流已输出后不能重新切号重放。
6. 等生产调用者迁完，再删除 provider 错误结构中的路由策略字段：`providers.Error.Failover` / `Cooldown`，以及 `providers.ClassifiedError` 中同类字段。保留表示上游事实的 `RetryAfter`；WorkBuddy 从响应解析出的重置时间改填这个字段，不删除这项上游信息。

**不改变的事情**

- provider 仍可把原生错误码转换成公共错误种类；“这是配额耗尽”是事实映射，“要不要切号、冷多久”才是 executor 策略。
- 保留 quota 不切号、冷却到本地午夜、Trae `4011` 硬限流、真实上游限流下限等现行规则。本轮不是重写错误分类表。
- 全部账号并发占满时，对外保留 executor 原定的 5 秒提示，是本步骤明确的行为修复，不伪称完全无行为变化。已有冷却的剩余时间也不得被当成新的上游错误再施加下限。
- `/v1/chat/completions`、`/v1/messages`、`/v1/responses`、`/api/chat` 的响应字段、协议错误形状及断流方式保持原有契约。

**完成标准**

- [x] 新增 HTTP 层回归：并发占满 → 429、`Retry-After: 5`，没有访问上游或新增账号冷却。
- [x] 已分类错误经过包装、HTTP 输出、日志记录后，分类字段保持一致；原始上游限流仍按现行策略分类。
- [x] 覆盖流中错误、缺少 `[DONE]`、上游读失败、客户端写失败、请求取消；四个聊天入口都覆盖适用的流式／非流式路径。
- [x] gateway 生产代码不再直接调用底层 `executor.Classify`，不再指定 failover；旧转换 helper 的生产引用清零。

### B. 模型设置只问 control，不让 HTTP 和数据库各管一半

**现在的问题**

`internal/console/models.go` 决定 provider 对应哪类设置、默认值和自定义状态；`internal/store/store.go` 的 `SetModelContext` 独自决定上下文长度范围；`control.Settings.SetModelContext` 只是转发。

**具体怎么改**

1. 给 `control.Settings` 增加完整操作，例如 `ReadModelSetting` / `UpdateModelSetting`，接收 provider、model 和结构化补丁，返回供 console 展示的结果。先迁入现有 provider 分派，不为此建立新的通用策略框架。
2. 将名称规范化、默认值回退、`context_custom` 计算、WorkBuddy 禁止 Max、补丁合并统一放在 control。console 只拆 URL、解码字段、调用一次服务、映射错误和输出响应。
3. 把上下文长度校验提成 `accounts` 中的纯函数，由 control 写入前调用。store 可以复用同一个函数防御非法写入，但不能再维护第二套阈值；`0` 删除设置的 SQL 仍留在 store。
4. 模型目录装饰复用相同默认值／有效设置规则，避免设置接口与目录显示各算一遍。不要让 control 反向依赖具体 provider 包。
5. 请求执行仍从现有设置读取接口获得默认值，不改变客户端显式值优先的规则；推理等级映射继续以 `providers/reasoning.go` 和模型目录声明为准。

**不改变的事情**

- 现有 URL、JSON 字段、默认窗口、清除设置方式、错误状态与错误码。
- 不顺手新增 Devin 设置能力，也不更改未知 provider、空 provider 或缺失字段的处理；先以现状契约测试固定，这类行为调整另列修复。
- 不移动 SQL 到 control，不改已发布 migration，不增加表。

**完成标准**

- [x] 领域测试覆盖 `-1 / 0 / 1023 / 1024 / 4000000 / 4000001` 等边界。
- [x] control 测试覆盖默认值、已有值、清除、补丁合并、WorkBuddy Max 拒绝、持久化失败。
- [x] HTTP 契约与模型目录显示一致；console 不再根据 provider 决定业务设置类型，不再独立推导有效默认值。

### C. 普通 API Key 由 control 创建，store 只接收可持久化的数据

**现在的问题**

`internal/store/api_keys.go` 的 `CreateAPIKey` 同时组织校验、生成明文、构造一次性返回值与 SQL；`UpdateAPIKey` 同时合并业务补丁与写库。`internal/control/keys.go` 基本只转发。这不同于合理的事务、ID 生成或哈希索引查询，应拆的是业务流程而非所有非 SQL 语句。

**具体怎么改**

1. 在 `accounts` 保留密钥生成、哈希、权限规范化等现有函数；补充可复用的创建校验和更新合并规则，明确 `Providers=nil` 是不改，空列表仍按现有契约处理。
2. `control.Keys.Create` 负责“校验 → 生成明文 → 计算哈希和前缀 → 请求保存 → 成功后返回一次明文”。生成失败不写库，保存失败不返回成功结果。
3. `control.Keys.Update` 负责读取、合并补丁、校验，再调用持久化操作；普通编辑不轮换密钥，不覆盖原哈希。
4. store 改为接收准备好的持久化参数，例如 `InsertAPIKey` / `SaveAPIKey`。新增内部记录类型时，不要把 `Secret` / `SecretOnce` 这类一次性响应字段带进数据库记录；查询结果也不能暴露哈希。
5. 同步调整 `accounts.AccountStore`、control 所需接口、fake 和调用方。业务用例测试迁到 control，store 测试只验证落库、读取、约束与未找到处理；不要为了旧测试在生产 store 中再保留一套密钥生成流程。

**不改变的事情**

- API Key 格式、哈希算法、权限语义、鉴权查询、前缀显示及“明文仅创建成功时返回”的行为。
- 不修改已有 `api_keys` 表和 migration。控制台主密钥轮换仍走 `control.KeyRotation`，不合并进普通 API Key 编辑。

**完成标准**

- [x] 创建后能鉴权；列表、详情和更新响应不泄露明文或哈希。
- [x] 空名称、非法权限、生成失败、保存失败都有 control 回归；更新保留原密钥。
- [x] `store/api_keys.go` 不再调用明文密钥生成器，也不再设置 `SecretOnce`；事务与数据库错误处理仍在 store。

### D. Qoder 告诉 runtime 怎么登录，runtime 不再猜 URL

**现在的问题**

`internal/runtime/manager_login.go` 的 `WorkerAdmin` 虽然调用了 `qoder.AdminAction`，却仍通过比较 `/admin/login/device`、`/admin/login/pat` 判断是否等待 AuthManager。

**具体怎么改**

1. 将 `qoder.AdminAction` 的结果改成动作描述，例如 `AdminSpec{Path, WaitForAuthManager, SyncAuth}`。具体路径、哪些动作需要等待、如何判断凭据同步时机，都由 qoder 包解释。
2. runtime 根据动作描述协调：找到账号 → 按需等待 → 调 worker → 成功后同步凭据。它仍负责运行状态和协调，但不再比较登录 URL，也不解析 OAuth 状态语义。
3. control / console 继续只传动作名。核对 `providers.AdminRequest.Path` / `SyncAuth` 的全部调用者：迁完后删除不再需要的透传字段，不能留可绕过动作描述的第二套生产入口。
4. 继续使用现有 `runtime → qoder` 白名单。不为消除几个路径字符串强推全量 Adapter 化，也不改变 worker HTTP 协议。

**完成标准**

- [x] qoder 表驱动测试覆盖 device、status、pat、rewarm、未知动作的路径／等待／同步要求。
- [x] fake-worker 测试确认等待超时、HTTP 失败、登录未完成、完成后凭据同步等行为保持一致；无真实账号也能验证。
- [x] 保留当前等待和请求超时设置、方法、请求体、响应体与错误映射；runtime 不再包含具体 `/admin/login/*` 路径判断。

### 防止以后又放回错误的包

现有 import 守卫继续保留，不扩充白名单来“让测试通过”。职责守卫增加结构检查与行为测试，而不是只追加几个函数名字的字符串匹配：

- A：检查 gateway 对底层分类函数的调用、provider 错误类型中的策略字段；关键保障是已分类错误往返与 HTTP Retry-After 测试。
- B：用 control 测试验证规则不依赖 SQLite，用 HTTP 契约测试验证 console 只是适配；store 的防御校验必须复用领域函数。
- C：检查 store 不调用密钥生成器、不构造一次性明文响应，并验证真实数据库里只落哈希。
- D：检查 runtime 的协议路径字面量，配合 qoder 动作表和 runtime 协调测试。

优先在已有测试文件／测试体系中补充，不新建一套通用架构检查框架。测试门面允许继续存在，但不能为兼容测试复制业务规则。

### 实施与验收清单

开工已 fetch/merge 最新 `origin/main`（Already up to date），并保留本分支已有未提交重构。A–D 已落地。

每一步先跑受影响包，再跑边界守卫：

| 步骤 | 定向验证 |
|---|---|
| A | `go test ./internal/executor ./internal/gateway ./internal/providers/... ./internal/api ./internal/app -count=1` |
| B | `go test ./internal/accounts ./internal/control ./internal/console ./internal/store ./internal/api -count=1` |
| C | `go test ./internal/accounts ./internal/control ./internal/store ./internal/auth ./internal/api ./internal/app -count=1` |
| D | `go test ./internal/providers/qoder ./internal/runtime ./internal/control ./internal/api -count=1` |

全部收口后统一验证：

```bash
go test ./internal/app -run 'TestImportConstraints|TestDutyBoundaries' -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/server ./cmd/updater
git diff --check
```

- [x] A 完成：调度错误与上游错误分离，gateway 不决定切号。
- [x] B 完成：模型设置规则收拢，原 HTTP 契约不变。
- [x] C 完成：普通密钥业务移入 control，鉴权和存储格式不变。
- [x] D 完成：Qoder 动作声明完整，runtime 不判断协议 URL。
- [x] 全量验证通过后，将本节各项勾选，并补充真实执行的命令与结果；不能沿用前文历史验收记录代替。

验证证据（2026-09-19）：

- [x] `go test ./internal/app -run 'TestImportConstraints|TestDutyBoundaries' -count=1`：通过。
- [x] `go test ./... -count=1`：通过。
- [x] `go test -race ./... -count=1`：通过，无 race 报告。
- [x] `go vet ./...`、`go build ./cmd/server ./cmd/updater`：通过。
- [x] `git diff --check`：通过。

本方案不处理已有的真实账号验收、托管更新发布、Restore API、完整 Adapter 切换或测试门面迁移；不改 frontend、worker 运行协议或 SQLite migration。除 A 明确列出的错误提示修复外，其余步骤以保持行为为前提。完成后也只宣称这五项已收口，不宣称所有架构债务清零。

### 复查修复：流错误与模型默认态（2026-09-19）

本轮先补永久回归测试，确认以下三处在修复前失败，再修改正式逻辑并验证通过。

| 问题 | 处理位置与方式 | 修复后的行为 |
|---|---|---|
| 流读取的取消／超时被当成上游不可用，误冷却账号 15 秒 | `executor.StreamReadError` 保留原因并识别取消；`ObserveStreamFailure` 跳过取消错误 | 即使入站请求上下文仍有效，也记为 canceled，不污染账号或模型冷却状态 |
| 类型化上游流错误绕过最终分类，后续层重复推导策略 | executor 将 provider 错误一次性封装为 `ExecutionError`；已有执行错误保留外层包装，普通传输错误也保留原因链 | HTTP 展示、日志和调度复用分类，`errors.Is/As` 仍能找到原始错误 |
| 选择目录默认推理等级后，刷新又显示“自定义” | `control.decorateProviderSettings` 按生效的 Max 与推理等级差异计算 `context_custom` | Trae、WorkBuddy 刷新前后状态一致；未生效的 Max 设置不会误标自定义 |

永久回归覆盖：

- `internal/executor/classify_test.go`：直接、包装、组合取消错误；上游错误只分类一次；已有执行错误及其包装保持不变；取消不修改 Pool 状态。
- `internal/api/chat_usage_test.go`、`compat_test.go`：OpenAI、Anthropic、Responses relay 返回最终执行错误，同时保留上游类型与原始原因。
- `internal/api/s01_protocol_contract_test.go`：取消／超时分别经过 `/v1/chat/completions`、`/api/chat`、`/v1/messages`、`/v1/responses`，验证不冷却、无额外上游调用，且请求日志记为 canceled。
- `internal/control/catalog_source_test.go`：保存自定义等级 → 选回目录默认 → 清空覆盖，重复刷新保持一致；区分支持、不支持以及 WorkBuddy 残留的 Max 设置。

本轮独立执行的验证结果：

- [x] 新增及加强的 executor、control、api 定向回归：通过。
- [x] `go test ./... -count=1`、`go test -race ./... -count=1`：通过，无 race 报告。
- [x] `go test ./internal/app -run 'TestImportConstraints|TestDutyBoundaries' -count=1`：通过，不扩充 import 白名单。
- [x] `go vet ./...`、`go build ./cmd/server ./cmd/updater`：通过。
- [x] `python3 scripts/release-notes.py validate`、`git diff --check`：通过。

仅验证本地自动化测试与构建；未进行真实账号验收、发布或部署。本轮不改前端资源、worker 协议或 SQLite migration。
