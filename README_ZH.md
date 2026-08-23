# CLI2API

[English](README.md) · [问题反馈](https://github.com/caigee-cmd/cli2api/issues) · [贡献指南](CONTRIBUTING.md)

[![CI](https://github.com/caigee-cmd/cli2api/actions/workflows/ci.yml/badge.svg)](https://github.com/caigee-cmd/cli2api/actions/workflows/ci.yml)
[![Docker Image](https://ghcr-badge.egpl.dev/caigee-cmd/cli2api/latest_tag?label=docker&color=blue)](https://github.com/caigee-cmd/cli2api/pkgs/container/cli2api)
[![License](https://img.shields.io/github/license/caigee-cmd/cli2api)](LICENSE)

一个非官方、自托管的 OpenAI 兼容 API 网关：复用你自己的 **Qoder CLI 登录态**，让支持 OpenAI API 的客户端连接到 Qoder。

CLI2API 不会为每个请求启动完整的 Qoder CLI Agent，而是让鉴权、WASM 编码和 Qoder 云端 HTTP/SSE 连接保持在常驻 worker 中。项目适合个人开发、家庭实验室和私有部署；目前仅支持 Qoder。

> [!IMPORTANT]
> CLI2API 与 Qoder 官方无关联，也未获得官方背书。请只使用你有权使用的账号，并遵守 Qoder 及相关服务条款。

## 工作方式

```text
OpenAI 客户端
  -> Go API + SQLite 账号控制面
    -> 每个 Qoder 账号一个隔离 Node worker
      -> Qoder 云端 HTTP/SSE API
```

每个启用账号拥有独立的 Node 进程和运行目录，避免共享 Qoder WASM 上下文。Go 负责账号持久化、调度、并发限制、冷却、失败切换和子进程生命周期。

## 功能

- OpenAI 兼容的 `POST /v1/chat/completions`
- 流式和非流式响应
- Tool calls 与 `reasoning_content`
- 多 Qoder 账号、账号固定、调度、冷却和故障切换
- 浏览器 Device Flow OAuth、PAT、`qoder-native-v1` 凭证导入/导出
- 内置 React + Tailwind + HeroUI 控制台，支持明暗主题
- 单容器 Docker Compose 部署
- SQLite 和账号凭证持久化，账号运行目录临时化
- GitHub Actions 自动测试、构建容器和发布 GHCR 镜像

![CLI2API 控制台](docs/assets/console.png)

## 快速开始

### 使用 Docker Compose

依赖：Docker、Docker Compose，以及一个你自己控制的 Qoder 账号。

一键启动脚本会自动创建 `deploy/.env`，启动已发布镜像（不可用时自动本地构建），并在首次启动时打印生成的 API Key：

```bash
git clone https://github.com/caigee-cmd/cli2api.git
cd cli2api
./scripts/start.sh
```

如果手动启动 Compose，可以将 `PROXY_API_KEY` 留空。服务首次启动时会生成密码学安全的随机密钥并写入 SQLite，同时在容器日志中打印一次。请先保存它，再配置客户端。

打开 `http://127.0.0.1:3010`，输入首次启动输出的密钥登录控制台，然后在 **Accounts** 页面添加 Qoder 账号。

默认 Compose 只发布本机地址 `127.0.0.1:3010`，不会直接暴露到公网。查看日志：

```bash
docker compose logs -f qoder-api-proxy
```

通过控制台创建的 Qoder 登录凭证会保存到 SQLite 的 `account_credentials` 表。Worker 运行时只在临时的账号运行目录中生成 Qoder 加密认证文件；Docker 部署会将该目录挂载为 tmpfs。

### 连接 OpenAI 客户端

将客户端配置为：

```text
Base URL: http://127.0.0.1:3010/v1
API Key:  <PROXY_API_KEY>
```

也可以直接测试：

```bash
export PROXY_API_KEY='粘贴首次启动时输出的密钥'

curl http://127.0.0.1:3010/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.7-plus",
    "messages": [{"role": "user", "content": "只回复 OK"}],
    "stream": false
  }'
```

如果需要固定使用某个账号，可以添加 `X-Qoder-Account: acc_...` 请求头；不指定时由调度器选择可用账号。

## 配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PROXY_API_KEY` | 可选 | 首次启动种子值；留空则自动生成并写入 SQLite |
| `QODER_DATA_DIR` | `/data` | SQLite 数据库和持久化账号凭证 |
| `QODER_RUNTIME_DIR` | `/run/cli2api` | 临时的账号 Qoder 运行目录 |
| `QODER_MAX_INFLIGHT` | `4` | 单账号最大并发请求数 |
| `QODER_WORKER_BASE_PORT` | `32100` | 内部 worker 端口起点 |

API Key 以 SQLite 中的值为准。`PROXY_API_KEY` 只用于首次启动时可选地提供种子值；数据库已有密钥后将忽略它。

## 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 健康检查，不需要 API Key |
| `GET` | `/v1/models` | 模型列表 |
| `POST` | `/v1/chat/completions` | OpenAI 兼容对话接口 |
| `GET/POST` | `/api/*` | 控制台管理接口 |

除 `/health` 外，控制台和 API 都需要 SQLite 中保存的 API Key。

## 从源码开发

环境要求：Go `1.25.6+`、Node.js `20+`、npm 和 Docker（仅容器开发需要）。

```bash
# Go API
go test ./...
go vet ./...

# Qoder worker
cd worker
npm test

# 控制台
cd ../frontend
npm ci
npm run build
npm run lint
```

修改前端后运行 `npm run sync`，将构建结果同步到 Go 的嵌入静态资源中：

```bash
cd frontend
npm run sync
```

从源码构建并启动容器：

```bash
cd deploy
docker compose up -d --build
```

更多仓库规则和验证命令见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 文档

- [架构、登录、路由和控制台设计](docs/DESIGN.md)
- [当前里程碑与开发计划](docs/PLAN.md)
- [脱敏后的协议记录](docs/capture-notes.md)
- [Docker Compose 部署说明](deploy/README.md)
- [变更记录](CHANGELOG.md)
- [安全问题报告](SECURITY.md)

## 安全与隐私

- 不要在没有 `PROXY_API_KEY` 的情况下暴露服务。
- 不要提交 `.qoder`、Token、Cookie、登录 Blob、原始抓包或主机信息。
- 账号凭证导出是显式敏感操作，请妥善保管导出文件。
- 上游 API 或 Qoder CLI 更新可能导致兼容性变化；项目会固定并检查 qodercli 版本。
- 发现安全问题请不要直接公开 Issue，按 [SECURITY.md](SECURITY.md) 的方式私下报告。

## 贡献

欢迎提交 Issue、改进文档和 Pull Request。提交前请确认：

- 变更范围清晰，未引入新的组件库或不必要的服务依赖。
- 已运行与改动相关的测试和构建命令。
- Diff 中不包含 Token、登录态、原始协议抓包或真实部署信息。

详见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 许可证

[MIT](LICENSE)
