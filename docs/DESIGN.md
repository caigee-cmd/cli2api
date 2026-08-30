# CLI2API 前端设计规范

本项目是纯已登录控制台应用（登录页 + 内部页面），基于 React、Vite、**HeroUI v3**、Tailwind v4 和 Phosphor 图标。前端视觉以 [HeroUI](https://www.heroui.com/docs/react/components) 的默认主题、语义 token 和复合原语为准。不要为单个功能发明第二套颜色、圆角、字体或控件高度。

Reading this as: self-hosted ops console for operators, with a HeroUI-default product language, cold gray surfaces, one blue accent, Outfit + IBM Plex Mono.

## 唯一真相来源

- 主题来自 `@heroui/styles` 的 default light/dark。只允许在 [frontend/src/index.css](../frontend/src/index.css) 里设字体、控制台特有的状态点 / 运行时柱 / 发行说明排版，以及下文「选中态」那两处胶水。不要覆盖 `--accent`、`--background`、`--radius` 或按钮高度。
- 对话框、提示、卡片、Chip、Drawer、表单、搜索、分页、空状态、开关、Meter 等可访问 UI **必须**用 `@heroui/react`。没有对应组件时，再查 [HeroUI 组件目录](https://www.heroui.com/docs/react/components)；最后才允许手写，并在 PR 里写明缺的是哪个原语。
- 图标用 `@phosphor-icons/react`，不要混入其他图标库。
- 颜色、圆角、字号一律走语义 token / Tailwind 映射（`bg-surface`、`text-muted`、`rounded-lg`）。禁止 `text-[var(--muted)]`、一次性 hex、页面私有色板。

## 视觉方向

控制台对齐 HeroUI 文档站点的产品 UI，而不是营销页：

- 冷中性灰表面（`--background` / `--surface`），不是暖象牙色、不是紫
- 唯一交互强调色是 `--accent`（HeroUI 默认蓝，约 `#0485F7`）
- 成功 / 就绪走 `--success`，警告走 `--warning`，破坏走 `--danger`
- 圆角跟下面的四档尺度，不要页面各画一套
- 控件用 HeroUI 默认尺寸：按钮 `size="sm"` 在桌面约 32px，默认 `md` 约 36px
- 实用优先于装饰；不要毛玻璃、光斑、彩色堆叠面板或套娃卡片

避免：

- 在应用壳里搞营销页式大标题
- 自定义 `--app-*` 色板盖住 HeroUI token
- 手写红框、手写进度条、手写分页，而 HeroUI 已有 `Alert` / `Meter` / `Pagination`
- 原生 `type="number"`、原生 `<select>`、`window.alert()`
- 为每个功能加一个新强调色
- 紫 / 象牙 / Inter / 衬线体出现在控制台或品牌套件里
- 同一页里「选中是蓝、选中是白、选中是灰」混用

## 颜色 Token

用 HeroUI 语义 token 和 Tailwind 映射。浅色近似值只用于 SVG / manifest / `theme-color`，运行时 UI 仍走 token。

| 用途 | Token / class | 浅色近似 |
|------|----------------|----------|
| 页面背景 | `bg-background` / `--background` | `#F5F5F5` |
| 卡片 / 表面 | `bg-surface` / `--surface` | `#FFFFFF` |
| 次级表面 | `bg-surface-secondary` | `#EFEFF0` |
| 正文 | `text-foreground` | `#18181B` |
| 次级文字 | `text-muted` | `#71717A` |
| 主操作 | `bg-accent text-accent-foreground`（`Button` 默认 `primary`） | `#0485F7` |
| 选中填充 | `bg-accent-soft text-accent-soft-foreground` | accent 15% 透明 |
| 边框 | `border-border` | `#DEDEE0` |
| 分割线 | `border-separator` / `divide-separator` | |
| 聚焦 | `--focus`（等于 `--accent`） | |
| 成功 | `--success` / `Chip color="success"` | `#17C964` |
| 警告 | `--warning` | `#F5A524` |
| 危险 | `--danger` / `Button variant="danger"` | `#FF383C` |
| 品牌节点（仅 mark） | 固定 `#22D3EE` | 青点 |

深色由 `@heroui/styles` 提供：背景约 `#060607`，表面约 `#18181B`，正文 `--snow`。主题切换：`<html class="light|dark" data-theme="light|dark">`。不要再抄一份 ivory / charcoal / violet 变量。

硬编码 hex 只允许：

1. 品牌闪电上的青点 `#22D3EE`
2. SVG / PWA / `theme-color` 无法引用 CSS 变量时的上表近似值
3. 第三方供应商 mark（WorkBuddy / Trae / Qoder）保持对方品牌色

### 选中态（必须同一套）

「当前选中」在全控制台是同一种蓝，不是白底、也不是中性灰块。

| 场景 | 做法 |
|------|------|
| 主按钮、提交 | `Button` 默认 / `variant="primary"` → 实心 `--accent` |
| 分段筛选、页签、分页当前页、Option tile、Radio / Checkbox / Switch | `--accent-soft` 底 + `--accent-soft-foreground` 字；控件本身用实心 `--accent` |
| 侧栏当前页 | `bg-surface-secondary text-foreground`（这是位置，不是控件）。左侧 2px 指示条用 `--accent` |
| 状态 Chip / Meter / 流量图 | 只用 success / warning / danger，表示真实运行态，不当成选中色 |
| 工具图标底 | `bg-surface-secondary text-foreground`，不要 `bg-foreground text-background`，不要蓝色方块 |

HeroUI 3.2.4 的 `Tabs.Indicator` 会因 `SharedElementTransition` 崩溃，不要使用。页签选中改走 [frontend/src/index.css](../frontend/src/index.css) 里对 `.tabs__tab[data-selected="true"]` 的 accent-soft。分页当前页默认是 `--default` 灰，同样在 `index.css` 里改成 accent-soft，与 `ToggleButton` 对齐。这两处是允许的组件胶水，不要再加第三处主题覆盖。

## 圆角

`--radius: 0.5rem`（8px）由 HeroUI 提供，不要改。全站只准这四档：

| 档 | Token / class | 用在 |
|----|----------------|------|
| 胶囊 / 大壳 | `rounded-3xl`，卡片和 Modal 跟 HeroUI：`min(32px, var(--radius-3xl))` | 按钮、页签、分页、Chip（Chip 官方是 `rounded-2xl`，保持官方）、Card、Modal、空状态、页级区块外壳 |
| 字段 | `rounded-field`（`--field-radius` = 12px） | SearchField、Input、Select、NumberField |
| 内衬 | `rounded-xl`（12px） | 侧栏 nav 行、Option tile、侧栏页脚小结 |
| 井 / 控件 | `rounded-lg`（8px） | 代码井、内嵌列表、关闭按钮、工具图标底、表单分组、骨架块 |

例外：额度细条、运行时柱可用 `rounded-[1px]` / `rounded-[2px]`。不要 `rounded-md`、不要同一页外壳有的 `rounded-lg` 有的 `rounded-3xl`。

## 字体

只准两族，在 [frontend/index.html](../frontend/index.html) 加载、在 `index.css` 的 `@theme` 里声明：

| 角色 | 字体 | 字重 |
|------|------|------|
| 界面 | Outfit，中文回退 PingFang SC / Microsoft YaHei | 400 / 500 / 600 |
| 数字、ID、代码 | IBM Plex Mono（`.mono`） | 400 / 500 |

不要 Inter、不要衬线、不要第三族。层级：

| 角色 | 规格 |
|------|------|
| 顶栏标题 | `text-2xl font-semibold tracking-[-0.035em]` |
| 页内标题 | 同上，`h2` |
| 页内说明 | `mt-1 max-w-2xl text-sm leading-6 text-muted` |
| 区块标题 | `font-semibold tracking-[-0.015em]` |
| 正文 | `text-sm leading-6` |
| 元信息 / 标签 | `text-xs` 或 `text-[11px] text-muted` |
| 侧栏分组 | `text-[10px] font-semibold tracking-[0.12em] uppercase text-muted` |
| 等宽 | `.mono`，`font-variant-numeric: tabular-nums` |

登录页主标题可以到 `clamp(2.25rem, 4vw, 3.6rem)`，仅限 `/login`。应用壳里不要再放大。

## 间距与壳层

- 应用壳：`max-w-[1480px]`，页边 `px-4 sm:px-6 lg:px-8`
- 页内竖向：标题区 `space-y-6` 或 `space-y-4`（账号页更紧），区块 `gap-5`，账号 / 密钥网格 `gap-2.5`、`lg:grid-cols-2 xl:grid-cols-3`
- 页头与内容之间：标题区 `border-b border-separator pb-4`
- 侧栏展开 248px，收起 76px

表格：`Card` + `Table.ScrollContainer`，`text-sm`，表头 `text-muted`，行 `divide-separator`。

卡片用 `Header` / `Content` / `Footer` 槽。账号卡片保持操作台密度：单行身份，状态 Chip 只出现一次；额度用 `Meter`；运行状态仍用 12 格短柱。不要为了分组把卡片嵌套在卡片里。

## 组件选型

按这个顺序选：

1. `@heroui/react` 已有原语。控制台常用映射：

| 场景 | 原语 |
|------|------|
| 页面区块 | `Card`（`Header` / `Title` / `Description` / `Content` / `Footer`） |
| 低强调容器 | `Surface` |
| 工具栏按钮簇 | `Toolbar`（`isAttached`）+ `Button isIconOnly` |
| 筛选分段 | `ToggleButtonGroup` + `ToggleButton`（`selectionMode="single"`） |
| 搜索 | `SearchField`（`Group` / `SearchIcon` / `Input` / `ClearButton`） |
| 表单字段 | `TextField` + `Label` + `Input` + `Description` + `FieldError` |
| 数字 | `NumberField` |
| 开关 | `Switch`（`Content` / `Control` / `Thumb`），账号行用 `size="sm"` |
| 状态条 / 额度 | `Meter`（`Output` / `Track` / `Fill`） |
| 空列表 | `EmptyState` |
| 页级错误 | `Alert` |
| 破坏确认 | `AlertDialog` |
| 普通设置弹窗 | `Modal` `size="lg"` |
| 分页 | `Pagination` |
| 日志 / 模型表 | `Table` |
| 日志页签 | `Tabs`（`ListContainer` / `List` / `Tab` / `Panel`）。不要 `Tabs.Indicator` |
| 账号类型 | `RadioGroup` + `Radio`（≤6 用 tile；超过用 `Select`） |
| 移动端导航 | `Drawer` |
| 总览流量图 | Recharts `ComposedChart`（HeroUI 没有 Chart；对齐 shadcn/ui charts 的 Recharts 层）。颜色走 `--success` / `--danger`，入场用 GSAP |

2. 项目包装只放在 `frontend/src/components/ui/`，用于把 HeroUI 原语接到控制台状态，而不是另画一套皮肤。
3. 仍没有、或现有行为确实无法覆盖，才手写。手写必须说明缺的是哪个 HeroUI 组件。运行时 12 格短柱属于账号卡片特有可视化，可以保留。总览流量图用 Recharts，不要再手画 SVG path。

账号编辑等设置弹窗：

- 用 `Modal` `size="lg"`，不要用 `sm` 把名称、并发、优先级挤进窄卡片。
- 数字用 `NumberField`。
- 校验失败用 `Alert`。
- 表单用 `Form` + `Label` / `Description`。
- 页脚操作用默认 `Button` 尺寸。

删除、轮换密钥、清空日志用 `AlertDialog`，不要再手写一套确认 `Modal`。

## 布局与密度

壳层、标题、间距以「间距与壳层」为准。操作型界面保持紧凑，但控件几何跟 HeroUI，不要再写 `.button { height: 2rem }` 或 `md:h-8` 这类覆盖。

账号卡片：控制台刷新时保持挂载。名称、并发和优先级通过较宽的编辑弹窗修改。添加账号仍是两步向导：第一步选类型、名称和可选高级选项，第二步再选登录方式。类型 ≤ 6 用 `RadioGroup` tile，超过则用 `Select`。

## 加载

第一次进入页面、点刷新、改会打接口的筛选，结果区都要换成 HeroUI `Skeleton`，不要转圈、不要留下一排 `—`。

- 还没有数据：整页骨架（`frontend/src/components/ui/PageSkeletons.tsx`），壳层侧栏和顶栏保留。
- 已有数据后再请求：保留标题、筛选和主按钮，只把列表 / 图表 / 卡片网格换成对应骨架。
- 纯前端筛选（账号名、模型名本地过滤）不用骨架。
- 登录门不要用全屏 Spinner 挡住页面骨架。

## 边框与分割线

线条要克制。分割线适用表格行、侧边栏、复杂弹窗中不加就难以扫视的区域。表面已有 `shadow-surface` 时通常不需要再加粗边框。

## 动效

- 优先用 HeroUI 自带过渡（按钮 `scale`、Meter `width`、Switch 轨道）。
- 控制台状态动画保持在 180–220ms；GSAP 只用于 HeroUI 没有的可视化（运行时柱、流量图、侧栏指示条、页面 reveal）。
- 只动画 `transform` 与 `opacity`，Meter 填充除外（官方用 `width`）。
- React 中使用 `gsap.context()`，`gsap.matchMedia()` 处理 `prefers-reduced-motion`，卸载时 `revert()`。
- 避免装饰性循环动画。

## 图标

- Phosphor，标准 `size-4`。
- 纯图标按钮需要 `aria-label`。
- 不要手写 SVG 路径（品牌 mark 除外）。

## 文案

- UI 文案保持简短直接，走 [frontend/src/i18n/messages.ts](../frontend/src/i18n/messages.ts)。
- 新增 key 时同步维护 `en` 和 `zh`。
- 已登录控制台不要用营销话术，不要 emoji。
- 不要全大写宽间距 kicker（`LOCAL / PRIVATE`、`OPENAI COMPATIBLE`、`PROTOCOL`）。小节标题用普通 `text-xs font-medium text-muted`。
- 状态点是实心小点，不要 halo / pulse。没有实时状态就不要点。
- 不要用彩色左边条当强调；列表和错误靠文字与字重。

## 前端改动自查清单

- 是否先用了 HeroUI 原语？
- 颜色是否只走 `--background` / `--surface` / `--accent` / `--muted` / status，而不是 `--app-*`、紫、象牙或一次性 hex？
- 选中态是否是 accent-soft（页签、分段、分页、tile），而不是白底或灰底？
- 外壳圆角是否跟 Card（`rounded-3xl`），井 / 关闭按钮是否 `rounded-lg`，字段是否 `rounded-field`？
- 字体是否只有 Outfit + IBM Plex Mono？页标题是否 `text-2xl font-semibold tracking-[-0.035em]`？
- 设置弹窗是否用了 `Modal` + `Form` + `NumberField` / `Alert`？
- 破坏确认是否用了 `AlertDialog`？
- 搜索是否用了 `SearchField`，分段筛选是否用了 `ToggleButtonGroup`，分页是否用了 `Pagination`？
- 是否没有使用 `Tabs.Indicator`？
- 第一次进入、刷新、打接口的筛选是否都有骨架，而不是转圈或空白？
- 浅色和深色主题下是否都正常？
- 是否跑了 `npm run lint`、`npm run build`，以及 UI 改动后的 `npm run sync`？

## Favicon 套件与品牌资产

CLI2API 的浏览器图标、PWA 清单和社交卡走极简 line-icon 路线，与控制台克制的 HeroUI 表面一致。

### Mark 母题

阶梯状闪电 + 90° 方形缺口（暗示终端 cursor）+ 固定 `#22D3EE` 青色圆点（数据流）。形状含义：

- **闪电** = CLI → API 协议转换
- **缺口** = 终端 cursor / 命令行起点
- **青点** = 数据流通 / 状态指示

闪电描边走当前主题墨色（浅色 `#18181B`，深色 `#FCFCFC`），不要紫色。青点是品牌里唯一的固定色，不当成按钮或选中填充。

### 文件清单

| 文件 | 位置 | 用途 |
|------|------|------|
| `frontend/public/favicon.svg` | source | 主图标，`stroke="currentColor"` 主题自适应 |
| `frontend/public/favicon-dark.svg` | source | 显式浅墨反白描边（`#FCFCFC`），深色模式回退 |
| `frontend/public/apple-touch-icon.svg` | source | iOS 启动图标，180×180 冷灰底（`#F5F5F5`）圆角 |
| `frontend/public/og-card.svg` | source | 1280×640 社交卡（运行时 og:image），冷灰底 + 墨色闪电 |
| `frontend/public/site.webmanifest` | source | PWA 清单，`background_color` `#F5F5F5`，`theme_color` `#0485F7` |
| `internal/webui/static/*` | runtime | 同上副本，被 Go `//go:embed` 打包进二进制 |
| `docs/assets/overview-card.png` | docs | README 顶部概览卡（手工维护） |

### 设计约束

- 每个图标 SVG 必须 < 1KB（favicon 系列 < 500B）
- 单 path 优先，避免 filter / gradient / mask / 嵌入文字
- 主图标用 `stroke="currentColor"` 走主题色；只有品牌识别度必需的强调点用固定色（青点）
- 不在图标内放 emoji、文字、版本号
- stroke 1.75px、round line caps、round line joins
- 禁止 `#7C3AED`、`#A78BFA`、`#FAF7F2`、`#EDE9FE` 出现在品牌套件里

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

Console taste: HeroUI v3 default theme, one accent, four-step radii, Outfit + IBM Plex Mono. Components **must** be HeroUI.

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
| Console | SQLite `proxy_api_key` | `/login` | Overview, accounts, models, keys, logs, API test, updates |
| Client | SQLite `api_keys` | `/v1` only | Chat and model list, optionally limited to selected providers |
| Qoder | device-flow / PAT | `/accounts` per worker | Upstream chat for that HOME |

`/health` stays open. Console `/api/*` requires the administrator key. `/v1` accepts the administrator key or a named client key.

The console API key is stored in SQLite `app_secrets` under `proxy_api_key`. A blank database generates a random key on first startup; the key has no environment-variable bootstrap path. Named client keys live in `api_keys` and only unlock `/v1`. Empty `providers` means every family; a non-empty list is applied in auth, then passed to `PickRoute` as `AllowedProviders`. Do not put key lookup inside the scheduler.

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
not add its own cache. Console header refresh calls `GET /api/overview?refresh=1`, which
asks the daemon for `/admin/quota?refresh=1` and bypasses that 15s cache. Silent account
mutations still use the cached snapshot.

Go `refreshOne` fetches quota after health for hot/ready accounts and stores a
`QuotaSnapshot` on the pool item. Quota is display-only: fetch failures are swallowed and
never flip account readiness, cooldown, or scheduling. Qoder reads quota from the daemon;
WorkBuddy reads remaining credits from its billing meter API (`unit: credits`). An
in-process provider without a quota surface simply omits the block. The account card
renders HeroUI `Meter` with `remaining/total <unit>`, `--danger` at 100% / exceeded,
`--warning` at ≥80%, plus an optional add-on line. Quota uses the Meter width transition;
runtime columns still animate on state change (`power2.out`, 180–220ms) and do not loop.

Distinguish this account-level quota from the request-level `insufficient_quota` error kind:
that error means a per-request token/model limit, not a zero account balance.

## SQLite migrations

Go embeds numbered SQL in `internal/accounts/migrations.go`. On open, each filename is recorded in `schema_migrations` with the SHA-256 of the **raw Go string**, including the leading newline and every tab or space. Applied files are never re-run. A checksum mismatch panics in `api.New` and the process exits, so Docker `restart: unless-stopped` loops and `/health` never comes up.

This is the rule that `v0.2.19` broke: it retabbed `006_request_log_provider.sql` while adding `007`. Databases that had already applied 006 on `v0.2.18` refused to boot. The host updater rolled us1 back because health never reported `v0.2.19`.

Hard constraints:

- Never edit a shipped migration's SQL bytes. Whitespace, comments, quote style, and statement order all change the checksum.
- `gofmt` / indent of the surrounding Go is fine. Indenting the text inside the raw string is not.
- New columns, indexes, or tables go in the next numbered file (`008_…`). Do not append to an applied file.
- Do not `UPDATE schema_migrations` on a live database to make a rewritten file pass. That hides the mismatch and can leave hosts on different schemas.
- Do not delete, rename, or reorder applied filenames.

If a bad rewrite already shipped (as with 006 in `v0.2.19`):

1. Restore the original SQL bytes so new databases record the first checksum.
2. Put the mistaken checksum on that file's `legacyChecksums` so databases that recorded the rewrite can still open.
3. Pin both checksums in `internal/accounts` tests. `TestRequestLogProviderMigrationKeepsV0218Bytes` is the pattern.
4. Ship that as a new patch. Do not ask operators to patch SQLite by hand.

`legacyChecksums` is only for a checksum that already landed in published images. It is not a way to keep editing old SQL.

## Console IA

Keep the menu short. Login is a gate, not a nav item.

| Route | Nav | Job |
|-------|-----|-----|
| `/login` | no | Console password |
| `/` | Overview | Runtime pulse + request stats |
| `/accounts` | Accounts | Qoder login + pool |
| `/providers` | Models | Catalog + per-model context-window defaults; filter by provider and paginate |
| `/access` | Access | Base URL + quick chat; account and model dropdowns follow the selected account catalog |
| `/logs` | Logs | Request history + runtime process output |
| `/keys` | API keys | Named client keys with optional provider allowlists |
| `/system` | System | Next-version update, SQLite protection, console administrator key |
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
- Runtime snapshot accepts `account`, `level`, `q`, `limit`, and `offset`. Results are newest-first. The console `/logs` runtime tab paginates this list; live polling stays on page 1.

## Managed update

The console may update only to the first stable GitHub release greater than the running version. It never accepts a target version from the browser, skips releases, installs prereleases, or updates a development build.

The application container does not receive the Docker socket. A small host daemon owns Docker replacement and exposes only a fixed update operation. Linux uses a mounted Unix Socket. Docker Desktop on macOS and Windows uses a strong bearer token over a daemon bound strictly to `127.0.0.1`; the container reaches it through `host.docker.internal`. macOS runs the daemon as a per-user LaunchAgent, while Windows runs it as a current-user Scheduled Task so it shares the logged-in user's Docker Desktop context. The Go control plane checks releases, pauses new API traffic, drains in-flight requests, creates a verified SQLite snapshot under `/data/backups`, and then submits the exact next version to the daemon. The daemon pins the target image in `deploy/.env`, snapshots the container network attachments, recreates only `qoder-api-proxy`, reconnects any network omitted by Compose, verifies the `/data` mount identity and reported version, and restores the old networks, image, and SQLite snapshot if health checks fail. A rollback leaves `CLI2API_IMAGE` pinned to the previous semantic version instead of restoring a floating `latest` reference.

Release packaging keeps the application as a Linux container and publishes a `linux/amd64` + `linux/arm64` manifest. The host updater is released separately for Linux, macOS, and Windows on `amd64` and `arm64`, with one SHA256 manifest. Installers prefer the asset matching the running release, may use a newer protocol-compatible updater to bootstrap an older release that had no asset, and use local compilation only as a final fallback.

Maintainers do not create version tags manually. A serialized `workflow_dispatch` release waits for CI on the exact `main` commit, calculates the next patch after the latest published stable release, creates or resumes an invisible draft release, uploads all updater assets, builds a candidate multi-architecture image, promotes and verifies immutable version tags, and finally publishes the release. Mutable `latest` and release-series aliases move only after publication. Failed pre-publication runs leave a resumable draft rather than exposing an update to the console.

Release notes come from `CHANGELOG.md`, not generated commit lists. Maintainers write matching `### English` and `### 中文` bullets under `## Unreleased`; the workflow copies that bilingual body onto the GitHub Release. The console System page extracts the current UI language, renders the markdown (lists, inline code, links), and shows it in a box that grows with the notes then scrolls. After the release is public, freeze those notes under the new version heading through a pull request — `main` is protected, so the workflow cannot push the freeze commit itself. Operator steps live in `docs/DEVELOPMENT.md`.

The host boundary is explicitly versioned through `protocol_version`. Version `1` is current; version `0` is temporarily accepted for an older updater that omitted the field. Any other version is rejected before an update request is submitted. New updater releases must remain backward-compatible with the immediately previous application release so the latest-asset bootstrap path stays safe.

Useful ideas borrowed from sub2api:

- long-running system work is detached from the browser request lifetime;
- system operations are serialized and expose durable status;
- release checks use a fixed trusted repository and stable releases only;
- host-only privileged work crosses a narrow authenticated boundary;
- applied database migrations are immutable and checksum-verified. Editing the SQL bytes of a shipped file, including whitespace, panics existing databases on boot; add a new numbered file instead. `legacyChecksums` is only for a checksum that already shipped by mistake.
- release artifacts are built per OS/architecture and verified by checksum.

Ideas intentionally not copied:

- in-place executable replacement does not fit an immutable container;
- updating straight to the latest release would skip ordered SQLite migrations;
- arbitrary version rollback is outside this project's next-version-only scope;
- PostgreSQL `pg_dump`, Redis, S3 backup scheduling, billing, and multi-tenant controls do not fit this local SQLite gateway.

## Design system

The console follows the frontend design baseline in the first half of this doc, adapted to this Vite app.

### Surface and tone

- HeroUI default cool-neutral surfaces. Do not restore the ivory/charcoal overlay.
- Practical before decorative; the authenticated console must not read like a marketing page.
- Primary buttons use `--accent`. Green is reserved for success, ready, and enabled states.
- Use `shadow-surface` / `shadow-overlay` from HeroUI; do not combine heavy borders, colored fills, and strong shadows without a clear hierarchy.
- Use semantic tokens from `@heroui/styles`; do not add `--app-*` colors in page components.

### Stack and primitives

- React 19, Vite, Tailwind CSS v4, HeroUI v3.
- Use HeroUI compound parts (`Card.Header`, `SearchField.Group`, `Meter.Track`, `Pagination.Content`) instead of recreating them with divs.
- Icons use `@phosphor-icons/react`; do not reintroduce another icon library.
- Keep route structure and the existing auth / endpoint / executor / translate / api boundaries unchanged.

### Layout and density

- Use `mx-auto max-w-5xl`, `max-w-6xl`, or the existing shell max width.
- Page titles use `text-2xl font-semibold tracking-tight`; supporting copy is small and muted.
- Prefer compact grids, status strips, and HeroUI separators over nested cards.
- Tables use `Table.ScrollContainer`, compact cells, and muted headers.
- Full-height layouts use `min-h-dvh`, never `h-screen`.

### Interaction and motion

- Loading, empty, and error states are required for data surfaces (`Skeleton`, `EmptyState`, `Alert`).
- Header refresh is a user-initiated reload: show the page skeleton. Account create/login/rewarm/delete refreshes stay silent so the card does not disappear under the operator.
- Logs keep the filter chrome visible; request and runtime lists show a skeleton while filters, pagination, tab switches, or refresh are in flight. Runtime live polling stays silent.
- Labels sit above form controls; helper text stays quiet and errors sit below the field.
- Primary actions use HeroUI `primary`. Secondary actions use `secondary` / `ghost`. Header utilities sit in an attached `Toolbar`. Inline destructive actions use `danger-soft`; solid danger is reserved for `AlertDialog`.
- Pure icon buttons require `aria-label`.
- Prefer HeroUI transitions. GSAP is allowed for short functional console visualizations that HeroUI does not cover. Scope animations with `gsap.context()`, handle reduced motion with `gsap.matchMedia()`, and revert/kill every animation during cleanup.
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
| `frontend/src/components/account/` | Compact account card, runtime meter, quota fill |
| `frontend/src/pages/ProvidersPage.tsx` | Model catalog + context-window defaults |
| `frontend/src/pages/LogsPage.tsx` | Request history + runtime logs |
| `frontend/src/pages/SystemPage.tsx` | Managed next-version update |
| `frontend/src/components/layout/` | Shell / menu |
| `frontend/src/components/ui/` | HeroUI wrappers (search, pager, switch, empty state, alerts) |
| `internal/accounts/` | SQLite account repository, migrations, snapshots, scheduler, child lifecycle, request logs |
| `internal/logs/` | Runtime ring buffer and async request recorder |
| `internal/providers/` | Provider registry, route pools, in-process adapters (`workbuddy/`) |
| `internal/update/` | Release selection and updater client |
| `internal/updater/` | Host-side Docker replacement and rollback |
| `worker/src/daemon.mjs` | One-account Qoder runtime only |
| `worker/src/errors.mjs` | Error taxonomy |
| `internal/executor/chat.go` | Proxy → worker |
| `docs/PROVIDERS.md` | Multi-provider extension plan; WorkBuddy first |
| `docs/PROVIDERS_TRAE_SOLO.md` | Trae CN Solo in-process adapter survey; supersedes `docs/PROVIDERS_TRAE.md` |
