# Changelog

User-facing notes for GitHub Releases and the console update page.
Write each change in both `### English` and `### 中文` under `## Unreleased`.

## Unreleased

### English

- Label Qoder Global accounts instead of a generic cloud account, and show the official Qoder mark
- Refresh console chrome: cream/ink primary actions, with green reserved for success and ready states
- Publish bilingual GitHub release notes from `CHANGELOG.md`

### 中文

- 将账号类型从「云账号」改为「Qoder 国际版」，并显示官方 Qoder 图标
- 刷新控制台视觉：主操作为奶油色 / 墨色，绿色只用于成功和就绪状态
- 发布时从 `CHANGELOG.md` 生成中英双语 Release 说明

## 0.2.0 - 2026-08-23

### English

- Replace the supervisor-based pool with a Go-owned SQLite account registry
- Run one isolated Node daemon and HOME per enabled Qoder account
- Add account CRUD, browser OAuth, PAT, native credential import/export, cooldown and failover
- Move deployment to one container with persistent `qoder-data`
- Redesign the HeroUI console with responsive light and dark themes

### 中文

- 用 Go 管理的 SQLite 账号注册表替换 supervisor 进程池
- 每个启用的 Qoder 账号使用独立的 Node daemon 和 HOME
- 增加账号增删改查、浏览器 OAuth、PAT、原生凭证导入导出，以及冷却和故障切换
- 改为单容器部署，数据保存在 `qoder-data` 卷
- 用 HeroUI 重做控制台，并支持响应式浅色和深色主题

## 0.1.0 - 2026-08-22

### English

- OpenAI-compatible streaming and non-streaming chat
- Tool calls and reasoning passthrough
- Hot Qoder WASM/auth context with pinned qodercli compatibility checks
- React console and loopback-only Docker Compose deployment
- Upstream usage support with token estimation fallback
- Redacted protocol notes and secret-scanning CI

### 中文

- 兼容 OpenAI 的流式和非流式对话
- 支持工具调用和 reasoning 透传
- 常驻 Qoder WASM/鉴权上下文，并固定检查 qodercli 兼容性
- 提供 React 控制台，以及仅监听本机的 Docker Compose 部署
- 支持上游 usage，并在缺失时回退到 token 估算
- 脱敏协议记录，以及密钥扫描 CI
