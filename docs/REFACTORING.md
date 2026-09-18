---
id: cli2api-behavior-preserving-refactoring
title: 已验收的后端包边界
scope: [backend, package-boundaries]
status: accepted
read-when: 查现行包归属、剩余 import allowlist、或未宣称的真实账号限制时
summary: S00–S15 已验收。本文记录现行包图、测试门面、剩余 allowlist 和未宣称限制，不再作为待办清单。
related: [AGENTS.md, docs/ARCHITECTURE.md, docs/PLAN.md, docs/DEVELOPMENT.md]
last-updated: 2026-09-18
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
- 既有 race：`TestExecStarterConfigSnapshotConcurrentWithSetProxyURL`、`TestMaintenanceApplyConflictAndFailedAgentUnblock`、`TestSystemUpdateDoesNotBackupWhenNoNextVersionExists`、`TestPersistFailureKeepsDirtyEntryAndRetries`
- quota / login / chat 生产路径仍走 worker HTTP

回滚：无 schema 变化时，用同一 SQLite 启动上一验收二进制；共享分支用 revert，不改写历史。
