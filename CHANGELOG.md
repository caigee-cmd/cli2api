# Changelog

User-facing notes for GitHub Releases and the console update page.
Write each change in both `### English` and `### 中文` under `## Unreleased`.

## Unreleased

### English

### 中文

## 0.2.6 - 2026-08-25

### English

- Add a Logs console page with request history and live runtime output
- Record chat request metadata, failover attempts, tokens, and latency in SQLite
- Capture Go and per-account daemon stderr in a redacted in-memory ring for the console

### 中文

- 新增「日志」控制台页，包含请求历史和实时运行输出
- 将聊天请求元数据、故障切换尝试、token 与延迟写入 SQLite
- 把 Go 与各账号 daemon 的 stderr 捕获到脱敏后的内存环形缓冲，供控制台查看

## 0.2.5 - 2026-08-25

### English

- Add a provider registry and in-process WorkBuddy adapter so CN/Global accounts can share the same console without a Node worker
- Replace account-wizard dropdowns with stacked option tiles so Qoder Global, WorkBuddy CN, and WorkBuddy Global labels stay fully visible
- Use the official WorkBuddy mark for WorkBuddy accounts and keep the Qoder mark on Qoder accounts
- Replace the console brand with a CLI2API line-icon mark on login, sidebar, empty states, favicon, and share cards
- Flush the login password-visibility control to the field edge instead of a floating boxed chip

### 中文

- 新增账号类型注册表和进程内 WorkBuddy 适配器，国内版 / 国际版账号可共用同一控制台，无需 Node worker
- 创建账号向导改为整行平铺选项，Qoder 国际版、WorkBuddy 国内版、WorkBuddy 国际版标题完整显示
- WorkBuddy 账号使用官网标识，Qoder 账号继续使用 Qoder 标识
- 控制台品牌换成 CLI2API 线形图标，覆盖登录页、侧栏、空状态、favicon 和分享卡
- 登录页显示密码按钮贴合输入框右缘，不再浮成独立小方块

## 0.2.4 - 2026-08-24

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
- 部署改为单容器，并持久化 `qoder-data`
- 用 HeroUI 重构控制台，支持浅色 / 深色主题
