# CLI2API

> 当前代码先落地 **Qoder** provider（原 `qoder-api-proxy`）。仓库名改为 `cli2api`，方便后续扩展更多 CLI 登录态。

把本地 **Qoder CLI 登录态**直接打到 Qoder 云端 API，对外提供 **OpenAI 兼容接口**。

这不是 [avaritiachaos/qoder-proxy](https://github.com/avaritiachaos/qoder-proxy) / [foxy1402/qoder-proxy](https://github.com/foxy1402/qoder-proxy) 那种每次请求 `spawn qodercli` 的包装器。  
架构更接近 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)：热持有鉴权、协议转换、常驻执行上游 HTTP/SSE。

仅个人 / 自建。钉死 `@qoder-ai/qodercli@1.1.27`，对不上就直接退出。Cursor 不在 v0.1 范围。

```text
客户端 (OpenAI SDK / Codex / CherryStudio)
  -> qoder-api-proxy (:3010，Go + SQLite 控制面)
    -> 每个启用账号一个隔离 Node daemon
      -> https://api1.qoder.sh/.../agent_chat_generation?Encode=1
```

English version: [README.md](README.md)

## 控制台 UI

React + Tailwind CSS v4 + **HeroUI** 控制台。设计见 [`docs/DESIGN.md`](docs/DESIGN.md)，规则见 [`AGENTS.md`](AGENTS.md)，当前计划见 [`docs/PLAN.md`](docs/PLAN.md)。账号调度参考 [sub2api](https://github.com/Wei-Shaw/sub2api)，不搬它的商业网关。

```text
frontend/
  src/
    components/layout/   # AppLayout / AppSidebar / AppHeader
    pages/               # Login / Overview / Accounts / Models / Access
    api/ hooks/ i18n/
```

构建并嵌入 Go：

```bash
cd frontend && npm install && npm run sync
# 会把 dist 同步到 internal/webui/static
```

Docker 多阶段构建会先编译前端，再打包 Go binary。

## 功能

- OpenAI 兼容 `POST /v1/chat/completions`
- 流式 SSE（`text/event-stream`）与非流式 JSON
- Tool calling：请求传 `tools` / `tool_choice`，响应回 `tool_calls`
- 思考/推理透传（上游有时返回 `reasoning_content`）
- 模型别名映射（如 `qwen3.7-plus -> qmodel`）
- 鉴权自愈（`/admin/rewarm`，鉴权失败自动 rewarm）
- Docker Compose 部署（适合内网）
- 优先用上游 SSE usage/credits，没有才估算

## 为什么做这个

常见 CLI 包装代理慢，是因为每次请求都冷启动 agent CLI（经常 10s+），还可能暴露本地工作目录。

本项目保持热 WASM/鉴权上下文，直接打云端推理接口，延迟更接近上游本身。

## 环境要求

- 推荐使用 Docker + Docker Compose
- 一把真实的 `PROXY_API_KEY`
- 可选：已有 `~/.qoder` 登录态，用于首次迁移

## 快速开始

```bash
cd deploy
cp .env.example .env
# 设置真实 PROXY_API_KEY
docker compose up -d --build
```

单容器已经包含 Go、Node、固定版 qodercli 和前端。SQLite 与每账号 HOME 持久化在
`qoder-data` volume。启动后进入 `/accounts` 添加浏览器 OAuth、PAT 或
`qoder-native-v1` 账号。

关键变量：

| 变量 | 含义 |
|------|------|
| `PROXY_API_KEY` | 控制台与 `/v1` 共用 API Key |
| `QODER_DATA_DIR` | SQLite 与运行目录；Docker 内为 `/data` |
| `QODER_HOME` | SQLite 为空时一次性导入的旧登录态 |
| `QODER_WORKER_BASE_PORT` | 内部 daemon 起始端口 |

### 测试

```bash
curl -s http://127.0.0.1:3010/health

curl -s http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "stream": false,
    "messages": [{"role":"user","content":"只回复OK"}]
  }'
```

流式：

```bash
curl -N http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "stream": true,
    "enable_thinking": true,
    "messages": [{"role":"user","content":"12*8=？只要答案"}]
  }'
```

工具调用：

```bash
curl -s http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "stream": false,
    "tool_choice": "auto",
    "tools": [{
      "type": "function",
      "function": {
        "name": "exec_command",
        "description": "执行 shell 命令",
        "parameters": {
          "type": "object",
          "properties": {"cmd": {"type": "string"}},
          "required": ["cmd"]
        }
      }
    }],
    "messages": [
      {"role":"system","content":"需要时使用 tools，不要把假 tool call 写进正文。"},
      {"role":"user","content":"请用工具执行 pwd"}
    ]
  }'
```

## 管理界面

启动 proxy 后打开：

```bash
open http://127.0.0.1:3010/
```

打开 `/login`，用 `PROXY_API_KEY` 当控制台密码登录。配置了 Key 时，控制台 `/api/*` 和 `/v1` 都需要它。Qoder 浏览器登录 / PAT 在「账号」页，按 worker 分别登录。

页面：

- 概览：运行状态 + 端点
- 账号：Qoder 登录 + 进程隔离的 worker 池
- 模型：当前目录
- 接入：curl 示例 + 快速对话测试

源码在 `frontend/`，通过 `internal/webui` embed 进二进制。

## Docker

```bash
cd deploy
cp .env.example .env   # 设置 PROXY_API_KEY
docker compose up -d --build
```

注意：

- 默认创建私有 `cli2api` 网络，只发布 `127.0.0.1:3010`
- worker 需要能读到 Qoder 登录文件（通常挂载 `~/.qoder`）
- 示例明文模板是 `worker/last-plain.sample.json`；更完整的抓包模板用 `PLAIN_TEMPLATE_PATH` 覆盖

详见 [`deploy/README.md`](deploy/README.md)。

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | proxy 健康检查 |
| GET | `/v1/models` | 已知模型别名 |
| POST | `/v1/chat/completions` | OpenAI Chat Completions |
| GET | `/debug/auth-snapshot` | 鉴权快照（需 Key） |
| GET | `/debug/endpoints` | 解析后的上游 endpoint（需 Key） |
| GET/POST | `/api/*` | 控制台接口（与 `/v1` 同一把 Key） |
| POST | worker `/admin/rewarm` | 强制刷新鉴权/上下文 |

### 思考 / 推理

以下任一开启即可：

- `is_reasoning: true`
- `enable_thinking: true`
- `enable_reasoning: true`
- `thinking: ...`
- `reasoning_effort: "low|medium|high"`

流式返回 `delta.reasoning_content`（若上游有）。  
非流式返回 `message.reasoning_content`。

### 系统提示词行为

worker **不会**继承抓包模板里那份巨大的 Qoder agent system/tools（会撑爆多轮）。

但会保留调用方传入的 system：

- 顶层 `system`
- `messages[].role = system|developer`

因此网关注入的身份提示词仍然有效。

### Usage

如果嵌套 SSE 带 `usage` / `llm_model_result`，直接用上游值（`usage.source=upstream`）。  
否则仍估算 token（`usage.source=estimate`），保证计费有数字：

- 非流式：`usage`
- 流式：在 `[DONE]` 前附加最终 usage chunk

### 多账号

账号现在保存在 SQLite，并直接从控制台的「账号」页管理，不再配置
`QODER_HOMES`、worker URL 或账号 ID。

由于 Qoder WASM 是进程内单例，每个启用账号仍然独占一个 Node daemon 和 HOME；
但账号选择、轮询、冷却和失败切换只由 Go 调度器负责。支持：

- 浏览器 Device Flow OAuth
- PAT
- `qoder-native-v1` JSON 导入/导出

首次启动单容器时，如果 SQLite 为空，会自动导入挂载的旧 `QODER_HOME` 登录态。
客户端仍可用 `X-Qoder-Account` 指定账号。

## 目录结构

```text
cmd/server/          Go 入口
frontend/            React 控制台
internal/api/        HTTP handlers
internal/auth/       登录态相关
internal/endpoint/   endpoint 解析
internal/executor/   proxy -> worker
internal/translate/  OpenAI 请求结构
internal/webui/      嵌入的控制台资源
worker/              热 QoderContext auth/encode worker
deploy/              Docker
docs/                方案 / 抓包笔记
testdata/            脱敏样例
```

## 文档

- [`AGENTS.md`](AGENTS.md) — 给 agent 的硬规则
- [`docs/DESIGN.md`](docs/DESIGN.md) — 架构、登录、调度、控制台、设计系统
- [`docs/PLAN.md`](docs/PLAN.md) — 当前里程碑清单
- [`docs/capture-notes.md`](docs/capture-notes.md) — 脱敏协议笔记
- [`docs/next-prepareRequest.md`](docs/next-prepareRequest.md) — worker 启动笔记
- [`CONTRIBUTING.md`](CONTRIBUTING.md) / [`SECURITY.md`](SECURITY.md)
- 本地私有运维笔记（已 gitignore）：`docs/PRIVATE_DEPLOYMENT.md`

## 合规提醒

面向 **个人自用 / 自建**。

请不要：

- 无鉴权公网裸奔
- 把同一个登录态拿去多人商业中转
- 提交原始登录态、token、抓包明文

上游 ToS / 账号风险请自行评估。

## 当前状态

已可用于自建：

- 非流式 + 流式
- tool calls
- reasoning 透传
- Docker 内网部署
- 鉴权 rewarm / 自愈

仍在演进：

- Cursor provider

## License

MIT，见 [LICENSE](LICENSE)。
