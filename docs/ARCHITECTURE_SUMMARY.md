---
id: cli2api-architecture-summary
title: CLI2API Architecture Summary
scope: [backend, runtime, providers, routing, console]
status: canonical
read-when: 需要快速理解后端分层、请求路径、运行时和 provider 扩展边界时
summary: 面向贡献者和 AI 的精简架构摘要；详细协议、里程碑和本地运维资料不在本文维护。
related: [AGENTS.md, CONTRIBUTING.md, docs/DESIGN.md, docs/REFACTORING.md, docs/NATIVE_PROTOCOL_REQUESTS.md]
last-updated: 2026-11-24
---

# CLI2API 架构摘要

本文是给贡献者和 AI 使用的快速入口，不替代代码、测试和本地详细资料。
行为以当前代码和测试为准；包边界以 `AGENTS.md` 与 `docs/REFACTORING.md` 为准。

## 产品边界

CLI2API 是一个面向个人部署的 Go + Node 网关：

- 对外提供 OpenAI Chat Completions、Anthropic Messages、OpenAI Responses 和 Models 兼容接口。
- 管理多个上游账号，并按 provider、region、model、pin、会话粘性和冷却状态选号。
- 已存 PAT 的 Qoder 账号（`qoder-pat-v1`）登录、目录、配额、签到和聊天都在 Go 进程内完成，不为该账号启动 Node worker。仍携带旧 `qoder-native-v1` CLI blob 的账号保留每账号一个 Node child process，直到操作者用 PAT 重新登录。其余所有 provider 使用 Go 进程内 adapter，不启动 CLI。
- 稳定接入是 Qoder、WorkBuddy、Trae CN Work，与控制台账号选择器的分组一致；其余已注册 provider 都是实验性的，不承诺生产可用。provider 清单以 `internal/providers/registry.go` 为准，不要把本文或 README 里的枚举当作完整列表。
- 控制台负责账号、密钥、设置、目录、日志、登录、导入和更新操作。
- 这是个人网关，不实现计费、Redis 槽位、多租户商业网关或公开暴露的管理端口。

## 运行时分层

```text
cmd/server
  └── internal/app       进程组装、依赖注入
        ├── internal/server   路由、CORS、OPTIONS、maintenance、webui
        ├── internal/gateway  公有 /v1 协议 HTTP 与 SSE
        ├── internal/console  操作员 /api/* HTTP
        ├── internal/control  控制面编排与目录展示缓存
        ├── internal/runtime  账号生命周期、Qoder worker、刷新与维护循环
        ├── internal/executor 选号、请求准备、分类、冷却与 failover
        ├── internal/store    SQLite、迁移、持久化请求记录
        ├── internal/providers provider 注册、协议客户端、adapter
        ├── internal/netx    进程级 HTTP 传输池与上游连接复用
        └── internal/update   托管更新协调器
```

`internal/api` 仅是测试兼容门面，生产启动路径不使用它，也不能在其中新增业务。

## 请求路径

1. `server` 将公有 `/v1/*` 请求交给 `gateway`，将控制台 `/api/*` 请求交给 `console`。
2. `gateway` 校验并解析 OpenAI、Anthropic 或 Responses 输入，转换为公共请求模型。
3. `executor` 根据显式账号 pin、会话粘性和账号池选择 runtime，并执行请求准备。
4. provider adapter 或 Qoder worker 负责上游协议、认证、HTTP/SSE 或 Connect-RPC。
5. `executor` 负责错误分类、冷却、重试和 failover；`gateway` 只负责公共协议响应和流式输出。
6. 请求元数据写入日志；不要默认保存 prompt、completion、密钥或凭证内容。

公共协议和 provider 上游协议必须分离。不要在 `gateway` 中拼 provider payload，
也不要让 provider 包接收 `http.ResponseWriter` 或决定 executor 的冷却策略。

## 包职责

| 包 | 负责 | 不负责 |
|---|---|---|
| `accounts` | 账号实体、凭证契约、纯账号规则 | Pool、Manager、HTTP handler |
| `store` | SQLite repository、不可变 migration、持久化 | 路由策略、密钥业务生成 |
| `control` | 账号/密钥/设置/登录/导入/目录编排 | 公有协议转换、provider payload |
| `runtime` | 账号生命周期、进程表、刷新、维护循环 | 公共协议和选号策略 |
| `executor` | Pool、选号、prepare、分类、冷却、failover | SQLite、HTTP handler、具体 provider policy |
| `gateway` | `/v1/*` 校验、转换、SSE 和公共错误 | store、runtime Manager、具体 provider |
| `console` | `/api/*` 解码、授权、错误映射 | store、runtime Manager、具体 provider |
| `server` | 路由和 web UI | 业务策略、store、runtime Manager |
| `app` | 组装依赖和启动关闭 | 领域业务规则 |

## Provider 边界

- Qoder Global 和 Qoder CN 是同一个 `provider=qoder`，通过 `region` 区分，不创建新的 provider family。
- 已存 PAT 的 Qoder 账号不启动 child process。旧 `qoder-native-v1` 账号仍使用独立 HOME 和独立 child process，不共享 WASM context；聊天不加载 CLI bundle。不得为每个请求启动完整 CLI agent。
- Qoder 的 CLI / worker 兼容性版本固定在 `worker/src/compat.mjs`，不兼容时应明确失败。
- Qoder 聊天在 Go 进程内使用 COSY 客户端签名直连网关（[`internal/providers/qoder/cosy.go`](../internal/providers/qoder/cosy.go)）。协议版本只有一个来源：`cosy.go` 的 `COSYVersion` 取自 [`qauth.CosyVersion`](../internal/providers/qoder/qauth/fingerprint.go)，改版本只改那里。支持中国区（`gateway.qoder.com.cn`）和全球区（`api2.qoder.sh`，登录仍走 `api1.qoder.sh`）双端点。
- 所有非 Qoder provider 使用进程内 adapter，不复制 Qoder worker 生命周期，也不得改为每请求启动上游 CLI。
- CodeArts 的 OAuth PKCE 只用于换取短期 STS 凭据，聊天直连 `snap-access.cn-north-4.myhuaweicloud.com` 的 OpenAI 兼容端点。
- provider 负责上游事实映射；executor 负责是否切号、冷却多久和是否 failover。
- Provider 能力、模型目录和 reasoning level 必须以实际 catalog 声明为准，不凭空增加模型能力。

## 持久化和安全边界

- SQLite migration 一旦发布，SQL 字节不可修改；新增 schema 必须追加新的编号 migration。
- 供应商级设置（积分/百分比保底、代理、系统提示词处理、别名、可用模型、签到时间）按 `provider + region` 存在 `app_secrets` 的 `provider_settings_v1` 中。服务启动时只为已有账号的 provider region 补一次缺失策略，之后 `store.Get/List` 在读取时把策略叠加成 effective account，供 runtime、目录、签到和账号视图使用；账号行只作为身份与调度的事实源，历史供应商列即使存在也被策略覆盖。保存供应商策略只能刷新 live pool，不得把同一组值批量写回账号行。跨 Provider 聚合模型写入时也必须用同一策略校验成员模型，不能绕过控制台模型列表保存已禁用模型。
- 控制台密钥、客户端密钥和上游凭证是不同的认证边界，不能混用。
- API 返回中不得暴露原始 token、auth blob、密钥明文、host 路径或凭证 payload。
- 管理接口必须认证；默认只绑定本机，不要直接暴露 host 端口到公网。
- 不提交 `.env`、auth blob、token、raw capture、主机 IP、私有部署 runbook。

## 原生协议请求

三个公共入口与上游协议的对应关系、双路径分派，见 [原生协议请求的现状与后续](NATIVE_PROTOCOL_REQUESTS.md)。
该文档状态为 `current`，描述的是已落地的行为：`/v1/responses` 在候选账号的 adapter 实现 `NativeResponsesStreamer` 时保留原文走原生路径，目前只有 Codex 实现了该接口；`/v1/chat/completions` 和 `/v1/messages` 仍先译成 `ChatRequest`。不要把它读成待实施设计。

## 修改前检查

- 先读 `AGENTS.md`，再按任务需要读 `DESIGN.md`、`CONTRIBUTING.md` 或本摘要。
- 修改路由、冷却、粘性或错误分类时，以当前代码和测试为准，并检查 `executor` 边界。
- 修改 provider 时，不要绕过 adapter/runtime 契约，也不要把协议策略塞进公共 handler。
- 修改控制台 UI 时遵守 `docs/DESIGN.md`，并运行 `cd frontend && npm run sync`。
- 修改行为后运行受影响包测试，再运行 `go test ./...`；检查 `git diff --check`。

详细 provider 协议、真实账号验收、部署主机信息和抓包记录属于本地资料，
不作为本公共摘要的实现来源。
