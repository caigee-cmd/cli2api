# CLI2API 前端设计规范

本项目是纯已登录控制台应用（登录页 + 内部页面），基于 React、Vite、HeroUI v3、Tailwind v4 和 Phosphor 图标。前端改动应保持现有安静、功能导向、暖白象牙色的界面风格。不要为单个功能引入新的视觉语言。

## 唯一真相来源

- 全局主题 token 在 [frontend/src/index.css](../frontend/src/index.css)。
- 对话框、提示、卡片、Chip、Drawer、表单等可访问 UI 场景，优先用 `@heroui/react` 的 HeroUI 原语。
- 当前项目直接使用 `@heroui/react` 原语；布局与复用组件位于 `frontend/src/components/`。
- 主题 token 与视觉变量统一维护在 [frontend/src/index.css](../frontend/src/index.css)。

- 图标用 `@phosphor-icons/react`，不要混入其他图标库，除非项目中没有等效图标。

## 视觉方向

应用整体应给人以下感觉：

- 安静、紧凑、功能导向
- 暖白底色，而非冷灰
- 默认低对比度，直到需要聚焦/操作时才提升
- 实用优先于装饰
- 通过阴影微微抬升层次，而非用粗边框

避免：

- 在应用壳里搞营销页式布局
- 彩色堆叠面板
- 套娃卡片
- 无意义的渐变
- 在 dashboard 界面里放大号标题排版
- 毛玻璃、光斑/球体、装饰性色块
- 为每个功能都加一个新强调色

## 颜色 Token

用语义 token，不要硬编码一次性颜色。

推荐用法：

- 页面背景：`bg-background`
- 主表面/卡片：`bg-card`
- 次级 hover/微弱表面：`bg-surface-secondary`
- 正文：`text-foreground`
- 次级文字：`text-muted-foreground`
- 主操作：`bg-primary text-primary-foreground`
- 边框：`border-border`
- 表格分割线：`divide-[color:var(--separator)]`
- 聚焦：`focus:ring-ring/20` 或沿用现有 wrapper 行为

仅以下明确状态可用原始色值：

- 危险：`text-red-600`、`text-danger`、HeroUI danger 变体
- 代码块：深色中性背景可接受
- 微小的状态/强调点：通过 HeroUI `Chip` 或 `Alert` 实现

不要在现有暖色表面上叠加新的米色/灰色层，除非有明确的层级需求。通常一个次级表面就够了。

## 组件

可访问的组合组件用 HeroUI：

- `Modal`、`Alert`、`Card`、`Chip`、`Drawer`、`NumberField`
- 基于 HeroUI 的本地 `Button` 和 `Input` wrapper

构建新弹窗时：

- 标准流程优先用 HeroUI `Modal`。
- 仅在现有行为/布局有特殊要求时才手写对话框。
- 弹窗表面保持视觉扁平：一个背景、浅边框、适度阴影。
- 避免 header/body/footer 各用不同背景色。
- 分割线只在有助于快速扫视时才加，不要默认上下都加边框。

构建表单时：

- 文本输入用 `Input` wrapper。
- 标签应小而安静：`text-xs` 或 `text-sm font-medium text-muted-foreground`。
- Select 视觉上应与 Input 一致：`h-10 rounded-md border border-input bg-background px-3 text-sm`。
- 辅助说明用安静文字表达，不要包在灰色卡片里，除非确实是需要强调的 callout。

构建按钮时：

- 主操作：`Button` 默认样式。
- 次要/取消：根据权重选 `variant="ghost"` 或 `variant="outline"`。
- 危险操作：确认弹窗可用实心 `danger`；卡片/表格行内操作使用 `danger-soft` 或克制的红色 ghost。
- 纯图标按钮需要 `aria-label` 和 `title`。
- 行内操作如果图标按钮更清晰，就不要用下划线文字链接。

操作型控制台统一采用以下紧凑规格：

| 控件 | 尺寸 | 字号 / 图标 | 圆角 |
|------|------|-------------|------|
| 普通按钮 | 高 32px，水平内边距 12px | 12px / 500，图标 14px | 6–8px |
| 图标按钮 | 32 × 32px | 图标 14px | 6–8px |
| 输入框 | 高 32px | 12–13px | 8px |
| 状态 Chip | 高约 20px | 10px / 500 | 6px |
| 启停 Switch | 轨道 34 × 18px，滑块 14px，内缩 2px | 状态文字 11px / 500 | 全圆角 |

同一操作区不要混用 32px、36px、40px 三套高度。账号卡片、筛选栏、行内操作默认使用 32px 基线；只有主要表单和移动端触控场景可放宽到 36–40px。

## 布局与密度

应用页面通常使用：

- `mx-auto max-w-5xl` 或 `max-w-6xl`
- 页面标题：`text-2xl font-semibold tracking-tight`
- 副标题：`mt-1 text-sm text-muted-foreground`
- 首个内容块：`mt-6`
- 卡片/网格间距：`gap-4`

操作型界面保持紧凑。不要在已登录页面里加超大留白或 hero 式区块。

表格：

- 容器：`overflow-x-auto rounded-lg bg-card shadow-card`
- 表格文字：`text-sm`
- 表头：`text-left text-muted-foreground`
- 行分割线：`divide-y divide-[color:var(--separator)]`
- hover：`hover:bg-surface-secondary/35`
- 单元格：通常 `px-4 py-3`

卡片：

- 圆角 8px 或更小，除非现有组件已用更大值
- 账号/资源列表卡片优先使用浅边框和平面表面，不强制最小高度，不额外叠加强阴影
- 主卡片只有在确实需要抬升层级时才用 `shadow-card`
- 边框克制使用；避免边框 + 阴影 + 有色背景同时出现
- 不要为了简单分组把卡片嵌套在卡片里

## 边框与分割线

线条要克制。

分割线适用场景：

- 表格行
- 侧边栏分隔
- 命令面板搜索/列表分隔
- 复杂弹窗中不加会导致难以扫视的区域

避免：

- 给弹窗每个区块都加 `border-t` 和 `border-b`
- 把每条表单提示都包在有边框的盒子里
- 同一组件里用多种分割线颜色

如果表面已有阴影，通常不需要再加粗边框。

## 动效

动效应微妙、快速，并直接解释状态变化。

- 优先用现有缓动 token：`--ease-fluid`、`--ease-snap`、`--ease-settle`。
- 控制台状态动画保持在 180–220ms；常规位移用 `power3.out`，填充/缩放用 `power2.out`。
- 启停 Switch 用滑块 `x` 位移和轨道填充 `scaleX` 表达状态；按下时轨道最多缩到 `0.96`，滑块最多放大到 `1.06`。
- 只动画 `transform` 与 `opacity`，不要通过 `width`、`left` 或 margin 制造位移。
- React 中使用 `gsap.context()` 限定作用域，使用 `gsap.matchMedia()` 处理 `prefers-reduced-motion`，卸载时必须 `revert()` / kill tween。
- reduced-motion 下状态必须立即切换，动画时长为 0。
- 避免在 dashboard/工作流区域放装饰性循环动画。

## 图标

- 用 Phosphor 图标。
- 标准图标尺寸：`size-4`。
- 操作类图标：复制、显示/隐藏、使用、启用/禁用、删除、关闭。
- 非显而易见的操作，图标旁应配简短文字。
- 不要手写 SVG 路径。

## 文案

- UI 文案保持简短直接。
- 用户可见的应用文字用 [frontend/src/i18n/messages.ts](../frontend/src/i18n/messages.ts) 中的 i18n store。
- 应用内不要放说明性段落，除非完成任务所必需。
- 已登录控制台流程中不要用营销话术。

### 加 i18n key 的流程

当前字典集中在 `frontend/src/i18n/messages.ts`，新增用户可见文案时同步维护 `en` 和 `zh`。

1. 在 `frontend/src/i18n/messages.ts` 的 `en` 字典中加 key。
2. 在 `zh` 字典中补齐对应中文翻译。
3. 组件中通过 `useI18n()` 的 `t(key)` 访问。

## 前端改动自查清单

完成前端改动前：

- 该用 HeroUI 或现有本地 wrapper 的地方是否用上了？
- 是否复用了全局语义 token？
- 边框/分割线是否必要，还是用间距和排版就能解决？
- 设计是否足够紧凑，适合反复操作使用？
- 图标是否来自 Phosphor 且可访问？
- 浅色和深色主题下是否都正常？
- 是否跑了 `npm run lint` 和 `npm run build`？

## Favicon 套件与品牌资产

CLI2API 的浏览器图标、PWA 清单和社交卡走极简 line-icon 路线，与控制台"安静、克制、暖白"的设计语言保持一致。

### Mark 母题

阶梯状闪电 + 90° 方形缺口（暗示终端 cursor）+ 固定 `#22D3EE` 青色圆点（数据流）。形状含义：

- **闪电** = CLI → API 协议转换
- **缺口** = 终端 cursor / 命令行起点
- **青点** = 数据流通 / 状态指示

### 文件清单

| 文件 | 位置 | 用途 |
|------|------|------|
| `frontend/public/favicon.svg` | source | 主图标，`stroke="currentColor"` 主题自适应 |
| `frontend/public/favicon-dark.svg` | source | 显式 `#A78BFA` stroke，深色模式回退 |
| `frontend/public/apple-touch-icon.svg` | source | iOS 启动图标，180×180 暖白底圆角 |
| `frontend/public/og-card.svg` | source | 1280×640 社交卡（运行时 og:image） |
| `frontend/public/site.webmanifest` | source | PWA 清单 |
| `internal/webui/static/*` | runtime | 同上副本，被 Go `//go:embed` 打包进二进制 |
| `docs/assets/overview-card.png` | docs | README 顶部概览卡（手工维护） |

### 设计约束

- 每个图标 SVG 必须 < 1KB（favicon 系列 < 500B）
- 单 path 优先，避免 filter / gradient / mask / 嵌入文字
- 主图标用 `stroke="currentColor"` 走主题色；只有品牌识别度必需的强调点用固定色（青点）
- 不在图标内放 emoji、文字、版本号
- stroke 1.75px、round line caps、round line joins

### 修改流程

1. 改 `frontend/public/*` 源文件
2. `make sync` — 复制到 `internal/webui/static/` 并嵌入 Go 二进制
3. `make favicon-sync` — 等价于 `make sync`，只同步 favicon 套件到嵌入静态资源
4. `CHANGELOG.md` `## Unreleased` 加双语条目
5. 如果新增了静态资源文件，三处都要改：
   - `frontend/scripts/sync-static.mjs` 的 `for (const name of [...])` 白名单
   - `internal/api/server.go` 的 `s.mux.Handle(...)` 和 `/` 兜底白名单
   - `frontend/index.html` 和 `internal/webui/static/index.html` 的 `<link>` / `<meta>` 标签

---

# Qoder API Proxy 产品与运行时约束

以下内容保留当前项目的后端架构、协议边界、账号路由和部署约束；前端视觉与交互以本文前半部分的设计规范为准。

Canonical design and product notes for agents. Plans live in `docs/PLAN.md`. Hard rules live in `AGENTS.md`.

Reference backend: [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api).  
We keep borrowing **account routing, failover, cooldown, in-flight caps**.  
We do **not** copy billing, Redis concurrency, multi-tenant keys, or commercial sticky sessions.

Console taste: [design-taste-frontend-v1](https://github.com) adapted for a self-hosted ops console. Components **must** be HeroUI.

## Runtime

```text
Client (OpenAI SDK / Codex / CherryStudio)
  -> qoder-api-proxy (:3010)            # one container
    -> Go control plane                  # auth, SQLite, routing, console
      -> Node daemon per account         # isolated HOME + hot QoderContext
        -> region=global: https://api1.qoder.sh/.../agent_chat_generation?Encode=1
        -> region=cn:     https://gateway.qoder.com.cn/... (WASM-encoded URL)
```

Worker pins `@qoder-ai/qodercli@1.1.32` for `qoder/global`. Qoder CN
(`provider=qoder`, `region=cn`) is the same daemon with a second pin,
`@qodercn-ai/qoderclicn@1.1.32`, and HOME `.qoder-cn`. Needle mismatch
exits loudly. See `docs/PROVIDERS.md` 「Qoder CN」; do not spawn a full
CLI per request and do not invent a `qodercn` family.

The request path that builds plaintext payloads, calls Qoder WASM encode, forwards
HTTP/SSE, parses tools/reasoning, and resolves usage remains unchanged. The account
control plane may be replaced; the proven Qoder execution path must not be rewritten.

## Model catalog and routing

Each account daemon reuses the Qoder CLI's in-process QoderModelCatalog after auth
initialization. It resolves canonical lowercase public IDs from catalog display names to
Qoder native keys, and accepts native keys directly. Never invoke qodercli --list-models
per request or synthesize a native key from a display name.

The CLI maintains its encrypted on-disk catalog cache. The daemon keeps an account-local
in-memory snapshot for five minutes (QODER_MODEL_CATALOG_TTL_MS); concurrent refreshes
share one promise. GET /admin/models?refresh=1 forces a refresh and exposes display
names plus diagnostic native keys. A failed refresh serves the last snapshot as stale;
without a snapshot, the catalog is empty.

There are no hand-written model aliases, display-name maps, or product overrides. When
the dynamic catalog is unavailable, the worker returns an explicit catalog error rather
than guessing an upstream key or allowing Qoder to silently choose a fallback model.

Go keeps a per-account catalog snapshot on the pool item and uses it as the chat
route pool. `POST /v1/chat/completions` refreshes stale snapshots, then
`PickRoute` only selects accounts whose catalog contains the requested public
ID or native key. Accounts with an unknown catalog (nil snapshot) stay eligible
so a cold worker is not skipped. If no candidate serves the model, the request
returns `model_not_available` without calling a worker or cooling an account.
A worker-level miss still failovers to another eligible account, but it does
not put the healthy account into cooldown; the stale ID is dropped from that
account's snapshot so the next pick cannot repeat it.

## Protocol adapters

Public protocols and the Qoder upstream format are separate contracts. Do not let
OpenAI Chat Completions or Anthropic Messages handlers build Qoder payloads independently.

```text
/v1/chat/completions -> OpenAI adapter    -> canonical conversation
/v1/messages         -> Anthropic adapter -> canonical conversation
                                                   -> Qoder adapter
                                                   -> prepareInferRequest / Qoder cloud
```

The canonical conversation preserves text, thinking, tool calls/results, images, cache
metadata, stop reasons, and provider identifiers. Each public adapter owns only its
request validation and response/SSE mapping. The Qoder adapter owns pinned CLI/runtime
compatibility, model parameters, tool normalization, and upstream event mapping.

When qodercli changes, update the Qoder adapter and its version-aware compatibility
tests first; do not duplicate the change in both public protocol handlers. Keep
`/v1/chat/completions` stable while `/v1/messages` is added as a separate ingress.

Qoder CN is still the Qoder family (`provider=qoder`, `region=cn`): same Node
daemon, second pinned CLI. WorkBuddy is the in-process adapter. Later Cursor
must plug into the account registry as its own provider, not a third copy of
the Qoder worker. See `docs/PROVIDERS.md`.

`/v1/messages` should be implemented as a native Anthropic boundary, not as
Anthropic -> OpenAI -> Qoder string rewriting. Borrow sub2api's reversible tool
mapping, cross-turn state, orphan-result filtering, and history repair, but keep the
project's existing Qoder execution path and account model.
## Two logins

They are not the same password.

| Gate | Secret | Where | Unlocks |
|------|--------|-------|---------|
| Console | SQLite API key | `/login` | Overview, accounts, models, API test |
| Qoder | device-flow / PAT | `/accounts` per worker | Upstream chat for that HOME |

`/health` stays open. `/api/*`, `/v1`, worker `/admin/*` and chat require the API key stored in SQLite.

The console API key is stored in SQLite `app_secrets` under `proxy_api_key`. A blank database generates a random key on first startup; the key has no environment-variable bootstrap path.

## Account routing

Qoder WASM / AuthManager is process-global. One HOME = one worker process.

SQLite is the durable account registry. Go owns the database, scheduling, cooldown,
failover, and child lifecycle. Node daemons never select another account.

The service starts one Node daemon per enabled account. Each daemon receives a private
ephemeral runtime HOME materialized from its SQLite credential record. The SQLite
credential record is authoritative; the runtime files are derived working copies.
`QODER_HOMES`,
`QODER_WORKER_URLS`, and `QODER_ACCOUNT_IDS` are removed from the product flow.

Supported account onboarding:

- Qoder browser device-flow OAuth (Global and CN; CN host is qoder.com.cn)
- Qoder PAT login (CN token UI: `https://qoder.com.cn/account/integrations`)
- `qoder-native-v1` JSON import containing the encrypted `.auth/user` blob and its
  matching `machine_id` (Global lives under `.qoder`, CN under `.qoder-cn`)

Qoder login waits for the per-account worker to report `hasAuthManager` on `/health`
before proxying `/admin/login/device` or `/admin/login/pat`. `cmd.Start()` still
returns immediately after spawn; WASM init can take several seconds on a cold CN
CLI, so the first click must not race `ECONNREFUSED` or an uncaptured AuthManager.
A missing AuthManager during login is `not_ready` (503), not `auth`.

Arbitrary `access_token` / `refresh_token` JSON is not supported. Qoder credentials
also depend on private user material, organization data, encryption keys, and device
identity. The API never returns raw credentials except through the explicit export
action.

Clients may pin `X-Qoder-Account`. If that worker is cooling, sticky-escape to another ready worker.

Error taxonomy (do not treat every 429 as empty balance):

| Kind | Signal | HTTP | Fail over | Cooldown |
|------|--------|------|-----------|----------|
| quota | `insufficient_quota`, `#token-limit` | 429 | no | no |
| rate_limit | generic 429 / too many requests | 429 | yes | ~60s, honor Retry-After, cap 10m |
| auth | 401/403, FORBIDDEN | 401/403 | yes | ~30s + rewarm |
| not_ready | hot context missing, AuthManager not captured, worker not warm | 503 | yes | ~10s |
| model_not_available | catalog miss for the requested public ID | 400 | yes, only other accounts that serve the model | no |
| unavailable | transport / 5xx | 502/503 | yes | ~15s |

`QODER_MAX_INFLIGHT` default 4. WASM encode + rewarm share one lock; do not hold it across upstream fetch.

Do not expose host paths or tokens in `/api/accounts`.

### Account quota display

Each Qoder daemon exposes `GET /admin/quota` (console API key required). The daemon calls the
qodercli `qoderApi` singleton captured via the `quotaApi` needle in `worker/src/compat.mjs`,
which fetches `openapi:/api/v2/quota/usage` with a plain Bearer token — no WASM encode. The
CLI already caches this endpoint for 15s and de-dupes concurrent fetches, so the daemon does
not add its own cache.

Go `refreshOne` fetches quota after health for hot/ready accounts and stores a
`QuotaSnapshot` on the pool item. Quota is display-only: fetch failures are swallowed and
never flip account readiness, cooldown, or scheduling. Qoder reads quota from the daemon;
WorkBuddy reads remaining credits from its billing meter API (`unit: credits`). An
in-process provider without a quota surface simply omits the block. The account card
renders one progress bar with `remaining/total <unit>`, `--danger` at 100% / exceeded,
`--warning` at ≥80%, plus an optional add-on line.

Distinguish this account-level quota from the request-level `insufficient_quota` error kind:
that error means a per-request token/model limit, not a zero account balance.

## Console IA

Keep the menu short. Login is a gate, not a nav item.

| Route | Nav | Job |
|-------|-----|-----|
| `/login` | no | Console password |
| `/` | Overview | Runtime pulse + request stats |
| `/accounts` | Accounts | Qoder login + pool |
| `/providers` | Models | Catalog + per-model context-window defaults |
| `/access` | Access | Base URL + quick chat |
| `/logs` | Logs | Request history + runtime process output |
| `/system` | System | Next-version update + SQLite protection |
| `/auth` | redirect | Legacy → `/accounts` |

Public model IDs are lowercase request identifiers. Qoder CLI names remain display labels, while internal Qoder keys are shown only for routing diagnostics.

Do not bring back a separate Auth page.

## Request history and runtime logs

Console `/logs` is one page with two tabs. Keep the menu short: do not split them into two nav items.

| Surface | Storage | Owns |
|---------|---------|------|
| Request history | SQLite `request_logs` + `request_attempts` | One client chat request, failover attempts, tokens, latency, error kind |
| Runtime logs | In-memory ring (~2000 lines) | Go control-plane and per-account daemon stderr, still tee'd to container stderr |

Rules:

- Go owns request logging. Generate `request_id` in `handleChatCompletions`, write attempts from the executor failover loop, and finalize stream rows after SSE relay.
- Do not store prompt or completion bodies by default.
- Purge request history at 7 days or 20_000 rows, whichever comes first.
- Capture daemon output through `ManagerConfig.MaxLogWriters`; prefix lines with `[account={id}]`.
- Runtime ring redacts obvious secrets and is lost on restart. Docker compose logs remain the durable operator stream for first-boot API key recovery.
- `/api/logs/*` requires the SQLite API key.
- Request history list accepts `account`, `status`, `stream`, `error_kind`, `model`, `q`, `from`, `to`, `limit`, and `offset`. The console `/logs` page paginates this list and exposes those filters.
- `GET /api/logs/stats` aggregates counts, success rate, latency percentiles, tokens, error mix, and a time series for Overview. Windows are 1h / 24h / 7d; series buckets are 15 minutes, hourly, or daily.
- Runtime snapshot accepts `account` in addition to `level` and `q`.

## Managed update

The console may update only to the first stable GitHub release greater than the running version. It never accepts a target version from the browser, skips releases, installs prereleases, or updates a development build.

The application container does not receive the Docker socket. A small host daemon owns Docker replacement and exposes only a fixed update operation. Linux uses a mounted Unix Socket. Docker Desktop on macOS and Windows uses a strong bearer token over a daemon bound strictly to `127.0.0.1`; the container reaches it through `host.docker.internal`. macOS runs the daemon as a per-user LaunchAgent, while Windows runs it as a current-user Scheduled Task so it shares the logged-in user's Docker Desktop context. The Go control plane checks releases, pauses new API traffic, drains in-flight requests, creates a verified SQLite snapshot under `/data/backups`, and then submits the exact next version to the daemon. The daemon pins the target image in `deploy/.env`, snapshots the container network attachments, recreates only `qoder-api-proxy`, reconnects any network omitted by Compose, verifies the `/data` mount identity and reported version, and restores the old networks, image, and SQLite snapshot if health checks fail. A rollback leaves `CLI2API_IMAGE` pinned to the previous semantic version instead of restoring a floating `latest` reference.

Release packaging keeps the application as a Linux container and publishes a `linux/amd64` + `linux/arm64` manifest. The host updater is released separately for Linux, macOS, and Windows on `amd64` and `arm64`, with one SHA256 manifest. Installers prefer the asset matching the running release, may use a newer protocol-compatible updater to bootstrap an older release that had no asset, and use local compilation only as a final fallback.

Maintainers do not create version tags manually. A serialized `workflow_dispatch` release waits for CI on the exact `main` commit, calculates the next patch after the latest published stable release, creates or resumes an invisible draft release, uploads all updater assets, builds a candidate multi-architecture image, promotes and verifies immutable version tags, and finally publishes the release. Mutable `latest` and release-series aliases move only after publication. Failed pre-publication runs leave a resumable draft rather than exposing an update to the console.

Release notes come from `CHANGELOG.md`, not generated commit lists. Maintainers write matching `### English` and `### 中文` bullets under `## Unreleased`; the workflow copies that bilingual body onto the GitHub Release. The console System page extracts the current UI language, renders the markdown (lists, inline code, links), and shows it in a box that grows with the notes then scrolls. After the release is public, a follow-up commit freezes those notes under the new version heading so the next patch starts from an empty Unreleased section.

The host boundary is explicitly versioned through `protocol_version`. Version `1` is current; version `0` is temporarily accepted for an older updater that omitted the field. Any other version is rejected before an update request is submitted. New updater releases must remain backward-compatible with the immediately previous application release so the latest-asset bootstrap path stays safe.

Useful ideas borrowed from sub2api:

- long-running system work is detached from the browser request lifetime;
- system operations are serialized and expose durable status;
- release checks use a fixed trusted repository and stable releases only;
- host-only privileged work crosses a narrow authenticated boundary;
- applied database migrations are immutable and checksum-verified.
- release artifacts are built per OS/architecture and verified by checksum.

Ideas intentionally not copied:

- in-place executable replacement does not fit an immutable container;
- updating straight to the latest release would skip ordered SQLite migrations;
- arbitrary version rollback is outside this project's next-version-only scope;
- PostgreSQL `pg_dump`, Redis, S3 backup scheduling, billing, and multi-tenant controls do not fit this local SQLite gateway.

## Design system

The console follows the frontend design baseline in the first half of this doc, adapted to this Vite app.

### Surface and tone

- Warm ivory surfaces, low contrast, compact information density.
- Practical before decorative; the authenticated console must not read like a marketing page.
- Warm ivory / charcoal surfaces. Primary buttons are cream/ink, not green. Green is reserved for success, ready, and enabled states. Avoid purple, neon, colored stacks, decorative blobs, glass, and large gradients.
- Use shadows to lift important surfaces; do not combine heavy borders, colored fills, and strong shadows without a clear hierarchy.
- Use semantic app tokens from `frontend/src/index.css`; do not add one-off colors in page components.

### Stack and primitives

- React 19, Vite, Tailwind CSS v4, HeroUI v3.
- Use HeroUI for accessible buttons, cards, chips, modals, inputs, selects, tables, and loading states.
- Icons use `@phosphor-icons/react`; do not reintroduce another icon library.
- Keep route structure and the existing auth / endpoint / executor / translate / api boundaries unchanged.

### Layout and density

- Use `mx-auto max-w-5xl`, `max-w-6xl`, or the existing shell max width.
- Page titles use `text-2xl font-semibold tracking-tight`; supporting copy is small and muted.
- Prefer compact grids, status strips, `divide-y`, and `border-t` groupings over nested cards.
- Cards are for meaningful action groups only; use 8px-or-smaller radii in the console.
- Tables use horizontal overflow, compact cells, muted headers, separators, and subtle hover surfaces.
- Full-height layouts use `min-h-dvh`, never `h-screen`.

### Interaction and motion

- Loading, empty, and error states are required for data surfaces.
- Labels sit above form controls; helper text stays quiet and errors sit below the field.
- Primary actions use HeroUI default buttons with cream/ink fill. Secondary actions use bordered ghost/secondary chips; header utility actions sit in a clustered icon group. Inline destructive actions use `danger-soft`; solid danger is reserved for confirmation dialogs.
- Compact console controls use a 32px button/input baseline, 12px medium button text, 14px action icons, 20px chips, and 6–8px radii. Icon buttons are 32 × 32px.
- Compact enable switches use a 34 × 18px track, a 14px thumb with 2px inset, and an 11px state label.
- Pure icon buttons require `aria-label` and a useful title where the action is not obvious.
- Animate only opacity and transforms. State transitions stay within 180–220ms using `power2.out` / `power3.out`.
- GSAP is allowed for short functional console transitions. Scope animations with `gsap.context()`, handle reduced motion with `gsap.matchMedia()`, and revert/kill every animation during cleanup. Reduced-motion state changes are immediate.
- Do not add perpetual decorative loops to the console.

### Copy and accessibility

- UI copy is short, concrete, and stored in `frontend/src/i18n/messages.ts`.
- No emoji, filler marketing language, fake metrics, or unexplained operational jargon.
- Keep both light and dark themes readable. Preserve keyboard focus and screen-reader labels.

### Pre-flight

After UI edits:

1. Run `cd frontend && npm run lint`.
2. Run `cd frontend && npm run build`.
3. Run `cd frontend && npm run sync` so `internal/webui/static` stays in lockstep.
4. Verify `/health`, console auth, and the public console route after deployment.

## Files

| Path | Owns |
|------|------|
| `frontend/src/pages/LoginPage.tsx` | Console gate |
| `frontend/src/pages/AccountsPage.tsx` | Qoder login + pool |
| `frontend/src/pages/ProvidersPage.tsx` | Model catalog + context-window defaults |
| `frontend/src/pages/LogsPage.tsx` | Request history + runtime logs |
| `frontend/src/pages/SystemPage.tsx` | Managed next-version update |
| `frontend/src/components/layout/` | Shell / menu |
| `internal/accounts/` | SQLite account repository, migrations, snapshots, scheduler, child lifecycle, request logs |
| `internal/logs/` | Runtime ring buffer and async request recorder |
| `internal/providers/` | Provider registry, route pools, in-process adapters (`workbuddy/`) |
| `internal/update/` | Release selection and updater client |
| `internal/updater/` | Host-side Docker replacement and rollback |
| `worker/src/daemon.mjs` | One-account Qoder runtime only |
| `worker/src/errors.mjs` | Error taxonomy |
| `internal/executor/chat.go` | Proxy → worker |
| `docs/PROVIDERS.md` | Multi-provider extension plan; WorkBuddy first |
