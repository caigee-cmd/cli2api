---
id: cli2api-behavior-preserving-refactoring
title: 已验收的后端包边界
scope: [backend, package-boundaries, regression-tests]
status: accepted
read-when: 查现行包归属、剩余 import allowlist、或未宣称的真实账号限制时
summary: S00–S15 包图、测试门面、剩余 allowlist、未宣称限制，以及 2026-09-19 密钥/更新/Starter 回归修复与验证记录。
related: [AGENTS.md, docs/ARCHITECTURE.md, docs/PLAN.md, docs/DEVELOPMENT.md]
last-updated: 2026-09-19
---

# 已验收的后端包边界

S00–S15 行为保持拆分已完成。不要再按旧 `internal/api` 大包或本文历史阶段去改代码。现行硬规则在 [`AGENTS.md`](../AGENTS.md)，运行时契约在 [`ARCHITECTURE.md`](ARCHITECTURE.md)。

## 现行包图

```text
cmd/server → app.New
               ├─ server     路由、CORS、OPTIONS、maintenance、webui
               ├─ gateway    /v1/chat/completions /v1/messages /v1/responses /v1/models
               ├─ console    /api/* 操作员 HTTP；/api/chat 复用同一 gateway 执行路径
               ├─ update     Coordinator：job / maintenance
               ├─ executor   Pool / route / classify / prepare / failover
               ├─ control    账号/密钥/设置/备份编排；Catalog 展示缓存
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
| 账号 CRUD / keys / settings | `internal/control` |
| 子进程启停 / 恢复 | `internal/runtime` |
| Qoder HOME / worker 协议 | `internal/providers/qoder` |
| SQLite / 新表 | `internal/store` 新编号 migration |

## 剩余生产 import allowlist

守卫测试：`go test ./internal/app -run TestImportConstraints`。CI 先跑这一项。测试 import 不计入。

| 边 | 原因 | 删除条件 |
|---|---|---|
| `api` → `app` | 测试门面 | 测试全部迁出 `package api` 后可删门面 |
| `executor` → `qoder` | chat 仍走 worker HTTP `qoder.NewChatRequest` | 生产 chat 切到 Adapter |
| `runtime` → `qoder` | spawn / HOME / quota / catalog 仍直接调 qoder | 剩余能力切到 Adapter |
| `console` → `devin`/`trae`/`workbuddy` | 账号导入仍在 HTTP 层解码凭据 | 解码下沉到 provider 契约 |

## 未宣称完成

- 真实账号：Qoder CN L6、WorkBuddy、Trae T5
- 托管更新 apply；正式发布
- Restore API；pin 缺失仍回退 pool；Delete 不立刻清 `recovering[]`
- quota / login / chat 生产路径仍走 worker HTTP

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

仍保留一个明确的边界债务：`internal/app/workerproxy.go` 同时包含 worker 管理代理和展示目录聚合。它是启动装配层注入给 `gateway`/`console` 的兼容桥，当前行为已有 worker、模型目录和 HTTP 回归覆盖；后续若继续拆分，应把目录聚合下沉到 `control` 的抽象数据源，不能让 `control` 反向依赖 runtime 或具体 provider。
