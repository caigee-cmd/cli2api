# CLI2API

[English](README.md)

一个非官方、自托管的 OpenAI 兼容 API，复用你自己的 **Qoder CLI 登录态**。

CLI2API 不会为每个请求启动完整 CLI Agent，而是保持 Qoder 鉴权和 WASM 编码上下文常驻。目前只支持 Qoder。

```text
OpenAI 客户端
  -> Go API + SQLite 账号管理
    -> 每个启用账号一个隔离 Node daemon
      -> Qoder 云端 HTTP/SSE API
```

## 功能

- `POST /v1/chat/completions`，支持流式与非流式
- Tool calls 和 `reasoning_content`
- 多 Qoder 账号调度、冷却和失败切换
- 浏览器 Device Flow OAuth、PAT、`qoder-native-v1` 导入/导出
- 内置 HeroUI 控制台，支持明暗主题
- 单容器 Docker 部署，SQLite 持久化

![CLI2API 控制台](docs/assets/console.png)

## 快速开始

需要 Docker、Docker Compose 和一个 Qoder 账号。

```bash
git clone https://github.com/caigee-cmd/cli2api.git
cd cli2api/deploy
cp .env.example .env
```

在 `deploy/.env` 设置强密钥：

```env
PROXY_API_KEY=替换成随机强密钥
```

启动：

```bash
docker compose pull
docker compose up -d
curl http://127.0.0.1:3010/health
```

打开 `http://127.0.0.1:3010`，用 `PROXY_API_KEY` 登录控制台，然后在「账号」页添加 Qoder 账号。默认 Compose 只监听 `127.0.0.1:3010`。

如果 `${QODER_HOME:-$HOME/.qoder}` 已有 Qoder 登录态，首次启动且 SQLite 为空时会自动导入。

## API 示例

```bash
export PROXY_API_KEY='替换成随机强密钥'

curl http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "messages": [{"role": "user", "content": "只回复 OK"}],
    "stream": false
  }'
```

客户端可用 `X-Qoder-Account: acc_...` 固定账号；不传时由 Go 调度器选择可用账号。

## 配置

| 变量 | 默认值 | 用途 |
|------|--------|------|
| `PROXY_API_KEY` | 必填 | 保护控制台、`/api/*` 和 `/v1/*` |
| `QODER_DATA_DIR` | `/data` | SQLite 和账号运行目录 |
| `QODER_HOME` | 主机 `~/.qoder` | 可选的一次性导入来源 |
| `QODER_MAX_INFLIGHT` | `4` | 单账号并发限制 |
| `QODER_WORKER_BASE_PORT` | `32100` | 内部 daemon 端口起点 |

`dev-key` 等占位密钥只有设置 `ALLOW_INSECURE_API_KEY=1` 才能启动，不能用于可访问的服务器。

## 接口

| 方法 | 路径 | 用途 |
|------|------|------|
| `GET` | `/health` | 公开健康检查 |
| `GET` | `/v1/models` | 模型列表 |
| `POST` | `/v1/chat/completions` | OpenAI 兼容对话 |
| `GET/POST` | `/api/*` | 控制台 API |

## 多账号模型

Qoder WASM 状态是进程级单例，因此每个启用账号独占一个 Node 进程和 HOME。Go 负责 SQLite、账号调度、冷却、失败切换和子进程生命周期。

普通账号接口不会返回原始凭证。导出凭证是显式操作，导出内容需要按敏感数据处理。

## 开发

```bash
go test ./...
cd worker && npm test
cd ../frontend && npm ci && npm run build && npm run lint
```

修改前端后运行 `cd frontend && npm run sync`，更新 Go 内嵌静态资源。仓库规则见 [CONTRIBUTING.md](CONTRIBUTING.md)。

需要从源码构建镜像时，运行 `cd deploy && docker compose up -d --build`。

## 安全与范围

- 仅用于个人自托管和你自己控制的账号
- 不要在没有 `PROXY_API_KEY` 的情况下暴露服务
- 不要提交 `.qoder`、登录 Blob、Token、Cookie 或原始抓包
- 本项目与 Qoder 官方无关联，也未获得官方背书
- 上游 API 或 CLI 更新可能导致兼容性变化；项目会固定并检查 qodercli 版本

安全问题请查看 [SECURITY.md](SECURITY.md)。

## 文档

- [架构与行为](docs/DESIGN.md)
- [当前里程碑](docs/PLAN.md)
- [脱敏协议笔记](docs/capture-notes.md)
- [Docker 部署](deploy/README.md)

## License

[MIT](LICENSE)
