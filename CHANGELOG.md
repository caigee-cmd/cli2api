# Changelog

User-facing notes for GitHub Releases and the console update page.
Write each change in both `### English` and `### 中文` under `## Unreleased`.

## Unreleased

### English

### 中文

## 0.2.12 - 2026-08-27

### English

- Add a per-account drop-system-prompt switch (on by default, WorkBuddy accounts): caller system prompts are stripped before provider-native chat so upstream content screening no longer rejects them
- Treat upstream content-screening rejections as request-level errors: they return 400 without failing over to other accounts or putting the account into cooldown
- Managed update now jumps directly to the latest stable release instead of advancing one release at a time; the console System page lists every intermediate version the update passes over

### 中文

- 新增账号级「丢弃系统提示词」开关（默认开启，WorkBuddy 账号）：请求发出前剥离调用方系统提示词，避免被上游内容审核拒绝
- 上游内容审核拒绝改按请求级错误处理：直接返回 400，不再向其他账号无谓切换，也不给账号打冷却
- 管理更新改为直接升级到最新稳定版本，不再逐版本前进；控制台 System 页会列出更新经过的全部中间版本

## 0.2.11 - 2026-08-27

### English

- Add Qoder CN accounts as `provider=qoder` + `region=cn`, using pinned `@qodercn-ai/qoderclicn@1.1.27` and `.qoder-cn`
- Wait for the Qoder worker AuthManager before browser or PAT login, so the first click does not fail while WASM is still starting
- Stop locally rejecting oversized Qoder prompts; let the upstream quota or context limit decide
- Keep Qoder failover inside the same region so Global 429s do not land on CN
- Send CodeBuddy CLI 2.139.0 channel headers on WorkBuddy chat, with CN and Global Origin/host kept separate
- Swap the repository front page to the Chinese README; the English README now lives at `README_EN.md`
- Replace the social card and console screenshot with a single overview card (`docs/assets/overview-card.png`) in both READMEs
- Redesign both READMEs around the console design language: add project-native SVG hero, console window, and architecture visuals, and reorder sections so quick start and client setup lead
- Lead both READMEs with a one-line positioning blurb and a bold feature list, and add a user-facing Roadmap section backed by `docs/PLAN.md`
- Slim the READMEs by moving configuration, endpoints, and managed-update details into `deploy/README.md` and the development/release workflow into `docs/DEVELOPMENT.md`
- Plan Phases M (session-sticky routing), N (WorkBuddy check-in and keepalive), and O (more upstream channels)

### 中文

- 支持添加 Qoder 国内版账号（`provider=qoder`，`region=cn`），使用 pinned `@qodercn-ai/qoderclicn@1.1.27` 和 `.qoder-cn`
- Qoder 浏览器 / PAT 登录会先等 worker AuthManager 就绪，避免第一次点击时 WASM 还在启动就报错
- 取消 Qoder 本地超大 prompt 预检，过大请求改由上游额度 / 上下文限制处理
- Qoder 故障切换限制在同一 region，国际版 429 不会打到国内版
- WorkBuddy 聊天补齐 CodeBuddy CLI 2.139.0 通道头，国内版 / 国际版 Origin 与 host 仍分开
- 仓库首页改为中文 README，英文 README 移至 `README_EN.md`
- 两个 README 顶部的社交卡和控制台截图替换为一张概览卡（`docs/assets/overview-card.png`）
- 按控制台设计语言重做两个 README：新增项目原生 SVG hero、控制台窗口与架构图，并重排章节，让快速开始与接入说明前置
- 两个 README 开篇改为一句话定位 + 加粗功能清单，并新增基于 `docs/PLAN.md` 的 Roadmap 小节
- 精简 README：配置、接口与托管更新细节移入 `deploy/README.md`，开发与发布流程移入 `docs/DEVELOPMENT.md`
- 计划新增 Phase M（会话粘性路由）、N（WorkBuddy 签到与保活）、O（更多上游渠道）

## 0.2.10 - 2026-08-26

### English

- Stop probing WorkBuddy accounts through empty-URL `/health`, so signed-in accounts stay ready on the Accounts page
- Show WorkBuddy remaining credits on account cards from the billing meter API

### 中文

- WorkBuddy 账号不再走空 URL 的 `/health` 探活，已登录账号在 Accounts 页保持就绪
- 账号卡片从计费接口展示 WorkBuddy 剩余积分

## 0.2.9 - 2026-08-26

### English

- Route WorkBuddy accounts through the in-process adapter instead of empty-URL Qoder workers
- Enable `CROSS_PROVIDER_MODEL_POOL` so bare model IDs can schedule across Qoder and WorkBuddy; `qoder/` and `workbuddy/` prefixes still pin one family
- Fail over across WorkBuddy accounts on rate-limit or unavailable errors, and return `X-CLI2API-Provider` from the selected account
- Label WorkBuddy CN/Global on account cards and show provider ownership in Access and Models

### 中文

- WorkBuddy 账号改为走进程内适配器，不再当成空 URL 的 Qoder worker
- 支持 `CROSS_PROVIDER_MODEL_POOL`，同名 bare 模型可在 Qoder 与 WorkBuddy 之间调度；`qoder/`、`workbuddy/` 前缀仍可钉死单一上游
- WorkBuddy 限流或不可用时在同类型账号间故障切换，并用实际选中账号返回 `X-CLI2API-Provider`
- 账号卡片正确显示 WorkBuddy 国内版 / 国际版，Access 与模型页展示所属供应商

## 0.2.8 - 2026-08-25

### English

- Emphasize quota percentage on account cards and show the remaining credits as smaller secondary text

### 中文

- 账号卡片突出显示额度百分比，剩余额度改为更小的次要文字

## 0.2.7 - 2026-08-25

### English

- Show each Qoder account's remaining credits and add-on quota on the account card
- Fetch account quota directly from the Qoder cloud API; quota outages never affect account readiness or scheduling

### 中文

- 账号卡片显示每个 Qoder 账号的剩余额度与附加包用量
- 额度直接来自 Qoder 云端 API；额度接口故障不影响账号就绪状态和调度

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
