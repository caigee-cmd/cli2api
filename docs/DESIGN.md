# CaiAI 前端设计规范

本项目是「公开营销页 + 已登录 SaaS 控制台」的双表面应用，基于 React、Vite、HeroUI v3、Tailwind v4 和 Phosphor 图标。前端改动应保持现有安静、功能导向、暖白象牙色的界面风格。控制台侧重克制与密度，营销页允许更大的排版与动效自由（见下文「营销表面」）。不要为单个功能引入新的视觉语言。

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

## 营销表面（公开页）

上面「视觉方向」里的密度要求与「避免营销式布局」条款**仅适用于已登录控制台**（当前项目为 `/`、`/accounts`、`/providers`、`/access`）。公开营销页（`/`、`/pricing`、`/docs`、`/about`、条款页）是另一张脸，允许更大的表达自由度，但底色、token、克制感不变。

允许：

- 大号 display 排版：`font-display` + `text-4xl → text-6xl`，仅用于营销页的 hero / 区块标题。
- 单一、低透明度、同色系的柔光 hero glow（如 `bg-foreground/[0.04] blur-3xl`），一个视口一个，不叠第二个。
- 一个交互式焦点物件（如首页的 `ApiKeyCard`），作为页面的触觉中心；必须遵守 `prefersReducedMotion()`，reduced-motion 下静止。
- `bg-gradient-notice` 营销条，用于公告 / 促销 / 充值提示这类「需要跳一下」的条带。
- GSAP 营销动效（进场、滚动揭示），快速且克制，遵守现有缓动 token。

仍然禁止：

- 彩色堆叠面板、套娃卡片
- 多个发光光斑、彩色光球、毛玻璃
- 无意义的大面积渐变背景
- 营销页里出现第二种强调色

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
- 危险操作：`variant="destructive"`，或表格行内操作用克制的红色文字。
- 纯图标按钮需要 `aria-label` 和 `title`。
- 行内操作如果图标按钮更清晰，就不要用下划线文字链接。

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
- 主卡片用 `shadow-card`
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

动效应微妙且快速。

- 优先用现有缓动 token：`--ease-fluid`、`--ease-snap`、`--ease-settle`。
- 用小幅度变换：`-translate-y-px`、透明度、短行揭示。
- 使用 GSAP 时遵守 `prefersReducedMotion()`。
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

---

# Qoder API Proxy 产品与运行时约束

以下内容保留当前项目的后端架构、协议边界、账号路由和部署约束；前端视觉与交互以本文前半部分的 CaiAI 设计规范为准。

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
        -> https://api1.qoder.sh/.../agent_chat_generation?Encode=1
```

Worker pins `@qoder-ai/qodercli@1.1.27`. Needle mismatch exits loudly.

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

- Qoder browser device-flow OAuth
- Qoder PAT login
- `qoder-native-v1` JSON import containing the encrypted `.auth/user` blob and its
  matching `machine_id`

Arbitrary `access_token` / `refresh_token` JSON is not supported. Qoder credentials
also depend on private user material, organization data, encryption keys, and device
identity. The API never returns raw credentials except through the explicit export
action.

Clients may pin `X-Qoder-Account`. If that worker is cooling, sticky-escape to another ready worker.

Error taxonomy (do not treat every 429 as empty balance):

| Kind | Signal | HTTP | Fail over | Cooldown |
|------|--------|------|-----------|----------|
| quota | `insufficient_quota`, `#token-limit`, oversized prompt | 429 | no | no |
| rate_limit | generic 429 / too many requests | 429 | yes | ~60s, honor Retry-After, cap 10m |
| auth | 401/403, FORBIDDEN | 401/403 | yes | ~30s + rewarm |
| not_ready | hot context missing | 503 | yes | ~10s |
| unavailable | transport / 5xx | 502/503 | yes | ~15s |

`QODER_MAX_INFLIGHT` default 4. WASM encode + rewarm share one lock; do not hold it across upstream fetch.

Do not expose host paths or tokens in `/api/accounts`.

## Console IA

Keep the menu short. Login is a gate, not a nav item.

| Route | Nav | Job |
|-------|-----|-----|
| `/login` | no | Console password |
| `/` | Overview | Runtime pulse |
| `/accounts` | Accounts | Qoder login + pool |
| `/providers` | Models | Catalog + per-model context-window defaults |

Public model IDs are lowercase request identifiers. Qoder CLI names remain display labels, while internal Qoder keys are shown only for routing diagnostics.
| `/access` | Access | Base URL + quick chat |
| `/auth` | redirect | Legacy → `/accounts` |

Do not bring back a separate Auth page.

## Design system

The console follows the CaiAI frontend design baseline, adapted to this Vite app.

### Surface and tone

- Warm ivory surfaces, low contrast, compact information density.
- Practical before decorative; the authenticated console must not read like a marketing page.
- Use one emerald accent on warm neutral surfaces. Avoid purple, neon, colored stacks, decorative blobs, glass, and large gradients.
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
- Primary actions use HeroUI default buttons. Secondary actions use ghost/outline variants.
- Pure icon buttons require `aria-label` and a useful title where the action is not obvious.
- Animate only opacity and transforms. Keep dashboard motion short and functional.
- GSAP is allowed for short entrance motion on the login surface; every animation must clean up and honor `prefers-reduced-motion`. Do not add perpetual decorative loops to the console.

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
| `frontend/src/components/layout/` | Shell / menu |
| `internal/accounts/` | SQLite account repository, scheduler, child lifecycle |
| `worker/src/daemon.mjs` | One-account Qoder runtime only |
| `worker/src/errors.mjs` | Error taxonomy |
| `internal/executor/chat.go` | Proxy → worker |
