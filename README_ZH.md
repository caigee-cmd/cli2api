# CLI2API

> 当前代码先落地 **Qoder** provider（原 `qoder-api-proxy`）。仓库名改为 `cli2api`，方便后续扩展更多 CLI 登录态。

把本地 **Qoder CLI 登录态**直接打到 Qoder 云端 API，对外提供 **OpenAI 兼容接口**。

这不是 [avaritiachaos/qoder-proxy](https://github.com/avaritiachaos/qoder-proxy) / [foxy1402/qoder-proxy](https://github.com/foxy1402/qoder-proxy) 那种每次请求 `spawn qodercli` 的包装器。  
架构更接近 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)：热持有鉴权、协议转换、常驻执行上游 HTTP/SSE。

仅个人 / 自建。钉死 `@qoder-ai/qodercli@1.1.27`，对不上就直接退出。Cursor 不在 v0.1 范围。

```text
客户端 (OpenAI SDK / Codex / CherryStudio)
  -> qoder-api-proxy (:3010)
    -> qoder-auth-worker (:3020, 热 QoderContext)
      -> https://api1.qoder.sh/.../agent_chat_generation?Encode=1
```

English version: [README.md](README.md)

## 控制台 UI

React + Tailwind CSS v4 + HeroUI（布局参考 Sub2API）：

```text
frontend/
  src/
    components/layout/   # AppLayout / AppSidebar / AppHeader
    pages/               # Overview / Auth / Providers / Accounts / Access
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

- Go 1.25+（proxy；Docker 用 `golang:1.25-alpine`）
- Node.js 20+（auth worker）
- 本机已有 Qoder 登录态（`~/.qoder`，由官方 Qoder CLI 登录产生）
- 可选：plaintext 模板，用于更完整的请求整形

## 快速开始

### 1) 配置

```bash
cp .env.example .env
```

关键变量：

| 变量 | 含义 |
|------|------|
| `PROXY_API_KEY` | 客户端调用时要带的 Key（`Authorization: Bearer ...`） |
| `QODER_WORKER_URL` | worker 地址，默认 `http://127.0.0.1:3020` |
| `QODER_WORKER_API_KEY` | proxy 调 worker 时使用的 Key |
| `QODER_HOME` | Qoder 目录，默认 `/root/.qoder` |
| `PLAIN_TEMPLATE_PATH` | 可选，worker 的 plaintext 模板 JSON |

### 2) 启动 worker

```bash
cd worker
PROXY_API_KEY=dev-key ALLOW_INSECURE_API_KEY=1 npm start
```

默认监听 `:3020`。pin 匹配时跳过 CLI `main`，热持有 `QoderContext`。

### 3) 启动 proxy

```bash
PROXY_API_KEY=dev-key ALLOW_INSECURE_API_KEY=1 QODER_WORKER_URL=http://127.0.0.1:3020 QODER_WORKER_API_KEY=dev-key go run ./cmd/server
```

默认监听 `:3010`。空/`change-me` 的 Key 会直接失败，除非 `ALLOW_INSECURE_API_KEY=1`。

### 4) 测试

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
- 授权：Qoder 登录态 / worker 热上下文 / rewarm
- 供应商：当前模型
- 账号：进程隔离的 worker 池
- API 接入：curl 示例 + 快速对话测试

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

Qoder WASM 是进程内单例，所以每个登录态要独占 HOME / worker 进程。

默认 Compose（一个 worker 容器，内部 supervisor）：

```bash
QODER_HOMES=acc1=/root,acc2=/home/acc2
# proxy 仍只配一个 URL；supervisor 负责 failover，并识别 X-Qoder-Account
QODER_WORKER_URLS=http://qoder-auth-worker:3020
```

独立 worker 容器：

```bash
QODER_WORKER_URLS=http://qoder-acc1:3020,http://qoder-acc2:3020
QODER_ACCOUNT_IDS=acc1,acc2
```

Chat 会 round-robin；rate-limit / 鉴权失败 / 未就绪 / 5xx 切下一个。  
`insufficient_quota`（经常是 prompt 过大，不是余额为 0）不会把整个池打穿。  
可用 `X-Qoder-Account` 钉死账号；该账号冷却中会 sticky-escape 到下一个就绪 worker。

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

- [`docs/PLAN.md`](docs/PLAN.md) — 实施方案与清单（先 Qoder，Cursor 后续）
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
