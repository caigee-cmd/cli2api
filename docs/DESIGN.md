---
id: cli2api-console-design
title: CLI2API 前端设计规范
scope: [frontend, console, design-system, heroui, tailwind]
status: canonical
read-when: 改控制台 UI / 颜色 / 圆角 / 组件选型 / 图标 / 文案 / favicon 时
summary: 控制台视觉与组件规范：token、圆角、字体、HeroUI 选型、布局密度、动效、图标、文案、前端自查清单、Favicon 套件。
related: [docs/ARCHITECTURE.md, AGENTS.md]
last-updated: 2026-09-19
---

# CLI2API 前端设计规范

本项目是纯已登录控制台应用（登录页 + 内部页面），基于 React、Vite、**HeroUI v3**、Tailwind v4 和 Phosphor 图标。前端视觉以 [HeroUI](https://www.heroui.com/docs/react/components) 的默认主题、语义 token 和复合原语为准。不要为单个功能发明第二套颜色、圆角、字体或控件高度。

Reading this as: self-hosted ops console for operators, with HeroUI compound primitives, cold zinc surfaces, Charcoal Ink chrome, Signal Cyan only on the C mark, Outfit + IBM Plex Mono.

## 唯一真相来源

- 主题来自 `@heroui/styles` 的 default light/dark。只允许在 [frontend/src/index.css](../frontend/src/index.css) 里设字体、把 `--accent` 收成 Charcoal Ink、把 `--accent-soft` 略加深、控制台特有的状态点 / 运行时柱 / 发行说明排版，以及下文「选中态」那两处胶水。不要覆盖 `--background`、`--radius` 或按钮高度。不要把 Signal Cyan 铺到按钮上。
- 对话框、提示、卡片、Chip、Drawer、表单、搜索、分页、空状态、开关、Meter 等可访问 UI **必须**用 `@heroui/react`。没有对应组件时，再查 [HeroUI 组件目录](https://www.heroui.com/docs/react/components)；最后才允许手写，并在 PR 里写明缺的是哪个原语。
- 图标用 `@phosphor-icons/react`，不要混入其他图标库。
- 颜色、圆角、字号一律走语义 token / Tailwind 映射（`bg-surface`、`text-muted`、`rounded-lg`）。禁止 `text-[var(--muted)]`、一次性 hex、页面私有色板。

## 视觉方向

控制台对齐 HeroUI 文档站点的产品 UI，而不是营销页：

- 冷锌灰表面（Canvas Gray / Pure Surface），不是暖象牙色、不是紫
- 交互强调是 **Charcoal Ink**（`--accent` = `#18181B`，深色翻成 Snow Stroke）：按钮、聚焦、选中。**Signal Cyan `#22D3EE` 只出现在 C 下唇和扫光**，不当按钮、不当大面积填充。HeroUI 默认蓝 `#0485F7` 不要出现
- 成功 / 就绪走 `--success`，警告走 `--warning`，破坏走 `--danger`；这些是状态，不是第二套品牌色
- 圆角跟下面的四档尺度，不要页面各画一套
- 控件用 HeroUI 默认尺寸：按钮 `size="sm"` 在桌面约 32px，默认 `md` 约 36px
- 实用优先于装饰；不要毛玻璃、光斑、彩色堆叠面板或套娃卡片

避免：

- 在应用壳里搞营销页式大标题
- 自定义 `--app-*` 色板盖住 HeroUI token
- 手写红框、手写进度条、手写分页，而 HeroUI 已有 `Alert` / `Meter` / `Pagination`
- 原生 `type="number"`、原生 `<select>`、`window.alert()`
- 为每个功能加一个新强调色（尤其不要蓝按钮 + 青 logo 并排抢）
- 紫 / 象牙 / Inter / 衬线体 / HeroUI 默认蓝出现在控制台或品牌套件里
- 同一页里「选中是青、选中是白、选中是灰」混用

## 颜色 Token

用 HeroUI 语义 token 和 Tailwind 映射。运行时 UI 走 token；hex 只给 SVG / PWA / `theme-color`、把 `--accent` 锁成 Charcoal Ink，以及 C 尖上的 Signal Cyan。色名按角色，不按冷暖感觉临时换一套。

### 色板与角色

| 名称 | Hex | 角色 |
|------|-----|------|
| **Canvas Gray** | `#F5F5F5` | 页面底。`bg-background` / `--background`。浅色近似，不是暖象牙 |
| **Pure Surface** | `#FFFFFF` | 卡片、字段、浮层。`bg-surface` / `--surface` / `--overlay` |
| **Quiet Well** | `#EFEFF0` | 次级表面、侧栏当前行、工具图标底。`bg-surface-secondary` |
| **Charcoal Ink** | `#18181B` | 正文、浅色 C 轨、浅色主按钮 / 聚焦 / 选中、PWA `theme_color`。浅色 `--accent`。禁止纯黑 `#000000` |
| **Muted Steel** | `#71717A` | 次级文字、说明、元信息。`text-muted` |
| **Hairline** | `#DEDEE0` | 结构线。`border-border` |
| **Off-Black Night** | `#060607` | 深色页面底。HeroUI dark `--background` |
| **Snow Stroke** | `#FCFCFC` | 深色 C 轨、深色主按钮填充、浅色按钮上的字。深色 `--accent` / 浅色 `--accent-foreground`。不是大面积页面底 |
| **Signal Cyan** | `#22D3EE` | **只在 C 下唇和扫光**。写死在 mark / favicon SVG 里，不进 `--accent`，不当按钮 |
| **Accent Soft** | 浅色 ink 28% / 深色 22% 透明 | 选中填充。`--accent-soft` + `--accent-soft-hover` 在 [frontend/src/index.css](../frontend/src/index.css) 里略加深；`--accent-soft-foreground` 仍由 HeroUI 从墨色 `--accent` 混出 |
| **Ready Green** | `#17C964` | 仅成功 / 就绪 / 已启用。`--success` |
| **Warn Amber** | `#F5A524` | 仅警告。`--warning` |
| **Break Red** | `#FF383C` | 仅破坏 / 错误。`--danger` |

深色由 `@heroui/styles` 提供：底 Off-Black Night，表面约 `#18181B`，正文 `--snow`。主题切换：`<html class="light|dark" data-theme="light|dark">`。浅色、深色、README 图、社交卡必须是同一套冷锌灰 + 墨色 chrome；青只点在 C 上。不要 ivory / violet 平行色板。

在 [frontend/src/index.css](../frontend/src/index.css) 把浅色 `--accent` 覆写成 Charcoal Ink、`--accent-foreground` 覆写成 Snow Stroke；深色对调，避免墨色按钮融进黑底。默认 `--accent-soft` 只有 12–15%，分段筛选选中态偏浅，所以浅色提到 28%、深色提到 22%。`--accent-soft-foreground` 仍由 HeroUI 从 `--accent` 混出来。不要再覆写 `--background` 或 `--radius`。

硬编码 hex 只允许：

1. `--accent` / `--accent-foreground` 这一处覆写，以及 SVG 里的 Signal Cyan / Charcoal Ink / Snow Stroke
2. SVG / PWA / `theme-color` 无法引用 CSS 变量时的上表近似值
3. 第三方供应商 mark（WorkBuddy / Trae / Qoder）保持对方品牌色

禁止出现在控制台、favicon、社交卡、README 图里：`#0485F7`、`#7C3AED`、`#A78BFA`、`#FAF7F2`、`#EDE9FE`、`#000000`、霓虹外发光。

### 选中态（必须同一套）

「当前选中」在全控制台是墨色 accent-soft，不是白底、也不是青色块，更不是 HeroUI 蓝。

| 场景 | 做法 |
|------|------|
| 主按钮、提交 | `Button` 默认 / `variant="primary"` → 浅色实心 Charcoal Ink、字 Snow Stroke；深色对调 |
| 分段筛选、页签、分页当前页、Option tile、Radio / Checkbox / Switch | `--accent-soft` 底 + `--accent-soft-foreground` 字；控件本身用实心 `--accent` |
| 侧栏当前页 | `bg-surface-secondary text-foreground`（这是位置，不是控件）。左侧 2px 指示条用 `--accent` |
| 状态 Chip / Meter / 流量图 | 只用 success / warning / danger，表示真实运行态，不当成选中色 |
| 工具图标底 | `bg-surface-secondary text-foreground`，不要青色方块 |

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
- 登录门不要用全屏 Spinner 挡住页面骨架。进行中的品牌操作（登录提交、添加账号轮询）用 `BrandMark loading`，沿 C 轨扫青光；列表刷新仍走骨架。

## 边框与分割线

线条要克制。分割线适用表格行、侧边栏、复杂弹窗中不加就难以扫视的区域。表面已有 `shadow-surface` 时通常不需要再加粗边框。

## 动效

- 优先用 HeroUI 自带过渡（按钮 `scale`、Meter `width`、Switch 轨道）。
- 控制台状态动画保持在 180–220ms；GSAP 只用于 HeroUI 没有的可视化（运行时柱、流量图、侧栏指示条、页面 reveal）。
- 只动画 `transform` 与 `opacity`，Meter 填充除外（官方用 `width`），品牌加载除外（沿 C 轨的 `stroke-dashoffset` 扫光）。
- React 中使用 `gsap.context()`，`gsap.matchMedia()` 处理 `prefers-reduced-motion`，卸载时 `revert()`。
- 避免装饰性循环动画。品牌扫光只在 `BrandMark loading` 时出现，并尊重 `prefers-reduced-motion`。

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
- 颜色是否只走 Canvas Gray / Pure Surface / Charcoal Ink / Muted Steel / status，青是否只出现在 C 上，而不是 `--app-*`、HeroUI 蓝、紫、象牙或一次性 hex？
- 选中态是否是墨色 accent-soft（页签、分段、分页、tile），而不是白底、灰底或青色块？
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

CLI2API 的浏览器图标、PWA 清单和社交卡走极简 line-icon 路线，与控制台同一套冷锌灰 + 墨色 chrome；青只点在 C 下唇。

### Mark 母题

单轨开口 C + Signal Cyan 下唇（被点亮，像停靠的包）。形状含义：

- **C 轨** = CLI2API 本身（一条轨，不是内外双环）
- **开口** = 终端 / 命令行入口
- **青尖** = 数据流通；加载时同一条轨上扫过 `#c-scan`

C 描边走当前主题墨色（浅色 Charcoal Ink `#18181B`，深色 Snow Stroke `#FCFCFC`）。青尖写死 `#22D3EE`，不跟 `--accent`。主按钮、聚焦环走墨色；C 下唇和扫光才是青。文档里强调 CLI 时可用开口里的 `>`（Prompt 变体）；控制台、favicon、社交卡用 Tip。

### 文件清单

| 文件 | 位置 | 用途 |
|------|------|------|
| `frontend/public/favicon.svg` | source | 主图标，C 轨 `stroke="currentColor"`，青尖固定 |
| `frontend/public/favicon-dark.svg` | source | 显式浅墨反白描边（`#FCFCFC`），深色模式回退 |
| `frontend/public/apple-touch-icon.svg` | source | iOS 启动图标，180×180 墨底（`#18181B`）圆角 + 反白 C |
| `frontend/public/og-card.svg` | source | 1280×640 社交卡（运行时 og:image），Canvas Gray + 墨色 C + 青尖；命令提示走墨色 |
| `frontend/public/site.webmanifest` | source | PWA 清单，`background_color` Canvas Gray `#F5F5F5`，`theme_color` Charcoal Ink `#18181B` |
| `internal/webui/static/*` | runtime | 同上副本，被 Go `//go:embed` 打包进二进制 |

### 设计约束

- 每个图标 SVG 必须 < 1KB（favicon 系列尽量短，两 path 的 C 大约 450B）
- 单轨优先，避免 filter / gradient / mask / 嵌入文字
- 主图标用 `stroke="currentColor"` 走主题墨色；强调点只用 Signal Cyan
- 不在图标内放 emoji、文字、版本号
- C 轨 `pathLength="100"`、round line caps、round line joins；加载扫光复制同一条轨
- 禁止 `#0485F7`、`#7C3AED`、`#A78BFA`、`#FAF7F2`、`#EDE9FE`、`#000000` 出现在品牌套件和 README 图里

### 修改流程

1. 改 `frontend/public/*` 源文件
2. `make sync` — 复制到 `internal/webui/static/` 并嵌入 Go 二进制
3. `make favicon-sync` — 等价于 `make sync`，只同步 favicon 套件到嵌入静态资源
4. 用户可见改动时在 `changelog/unreleased/` 加一个双语 fragment，不要改 `CHANGELOG.md` 的已发布章节
5. 如果新增了静态资源文件，三处都要改：
   - `frontend/scripts/sync-static.mjs` 的 `for (const name of [...])` 白名单
   - `internal/server/router.go` 的 `s.mux.Handle(...)` 和 `/` 兜底白名单
   - `frontend/index.html` 和 `internal/webui/static/index.html` 的 `<link>` / `<meta>` 标签
