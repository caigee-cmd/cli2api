# Changelog

Published user-facing notes for GitHub Releases and the console update page.
Write upcoming notes as bilingual files in `changelog/unreleased/`.

## 0.7.19 - 2026-10-28

### English

- Access page uses top-level Chat | Raw API tabs (no raw-API drawer).
- Chat feels like a playground: collapsible thinking, light tool blocks, stream segment fade-in, composer model/account bottom-right, and a collapsible in-page history rail.
- Add Account wizard stacks name and advanced fields (label above full-width controls); max concurrency and priority share a compact two-column row.
- Modal and provider tiles use solid-glass card tokens; picker is a 2-column tile grid with single-line truncated labels, and provider icons blend into the tile surface.
- Add Account dialog width is `36rem` with a shared `sm:min-h-[36rem]` so the picker and form steps stay similarly tall (body scrolls inside when content overflows).

### 中文

- Access 页改为顶层「聊天 | 原始 API」标签（去掉原始 API 抽屉）。
- 聊天侧具备 playground 手感：可折叠思考、轻量工具块、流式段落淡入、作曲区右下模型/账号，以及可折叠的页内历史侧栏。
- 添加账号向导改为标签在上、控件通栏堆叠；最大并发与优先级并排两列。
- 弹窗与提供商磁贴沿用 solid-glass 卡片质感；选择步为两列磁贴、单行截断标签，图标融入磁贴底色。
- 添加账号弹窗宽度 `36rem`，并设共用 `sm:min-h-[36rem]`，使选择步与表单步高度接近（内容超出时在弹窗体内滚动）。

## 0.7.18 - 2026-10-28

### English

- Bump Codex outbound cloaking User-Agent to official stable `0.165.0` (`rust-v0.165.0`). CPA pin stays at 0.154; 0.166 alpha held.
- WorkBuddy outbound sanitize now rewrites the DSH file-reference sentence (`paths the user` → `paths that the user`) on system / developer / assistant messages, matching hub v1.9.0 / #684 so the full FILE_REFERENCE_PROMPT block no longer trips 11-128. User and tool messages stay untouched.

### 中文

- Codex 出站伪装 User-Agent 升至官方稳定版 `0.165.0`（`rust-v0.165.0`）。CPA pin 仍为 0.154；暂不跟 0.166 alpha。
- WorkBuddy 出站清洗对齐 hub v1.9.0 / #684：对 system / developer / assistant 中的 DSH 文件引用句补入 `that`（`paths the user` → `paths that the user`），避免整段 FILE_REFERENCE_PROMPT 触发 11-128；user / tool 消息保持原样。

## 0.7.17 - 2026-10-28

### English

- Add Account login step: primary actions sit in the modal footer (no full-width ink bar); method chips match the console FilterToggle (labels only).

### 中文

- 添加账号登录步骤：主操作改到弹窗页脚（去掉通栏墨色条）；登录方式分段与控制台 FilterToggle 一致（仅文字）。

## 0.7.16 - 2026-10-28

### English

- Add Account picker shows a small CaiAI hosted soft link under the provider tiles (homepage, not a fake provider card).
- CaiAI empty-state soft link now opens the homepage (`https://caiaiu.com`) instead of the affiliate register URL.
- Add Account picker dialog is slightly narrower (`max-w-5xl`) while keeping the 3-column tile grid.
- Add Muse (Meta) as an experimental login-source provider (`meta`, alias `muse`): device-code OAuth against auth.meta.com, mint LLM keys at api.meta.ai, Responses chat with encrypted_content shape validation, and optional Meta Model API key paste.
- Console Add Account picker shows a Muse (Meta) tile; model prefixes accept `meta/` (and `muse/` resolves to `meta`).
- Muse (Meta) device-code login and API-key mint now use the account `proxy_url` when set (else global), matching chat/Responses and CPA credential→global priority.

### 中文

- 添加账号选择弹窗在提供商磁贴下方增加小号 CaiAI 托管软链（首页，非假提供商卡片）。
- 空态 CaiAI 软链改为打开首页 `https://caiaiu.com`，去掉注册页 affiliate 参数。
- 添加账号选择弹窗略收窄为 `max-w-5xl`，仍保持三列磁贴布局。
- 新增实验性登录源 Muse (Meta)（`meta`，别名 `muse`）：走 auth.meta.com 设备码授权、在 api.meta.ai 兑换 LLM Key，聊天走 Responses，并对 encrypted_content 做形状校验透传；也可粘贴 Meta Model API Key。
- 控制台「添加账号」出现 Muse (Meta) 磁贴；模型前缀支持 `meta/`（`muse/` 会解析到 `meta`）。
- Muse (Meta) 设备码登录与 API Key 兑换在设置了账号 `proxy_url` 时走账号代理（否则全局），与聊天/Responses 及 CPA credential→global 优先级一致。

## 0.7.15 - 2026-10-27

### English

- Add-account picker uses Magpie-style grouped provider tiles (Subscriptions / Experimental) for real login sources only.
- Empty Accounts (and Access when no accounts) show a skippable CaiAI hosted soft link, not a fake provider card.

### 中文

- 添加账号第一步改为 Magpie 风格的分组磁贴（稳定订阅 / 实验性），仅展示真实登录源。
- 账号列表为空时（Access 无账号时同理）显示可跳过的 CaiAI 托管软链，不做假 provider 卡片。

## 0.7.14 - 2026-10-27

### English

- Keep Trae caller system prompts by default, forward `response_format`, and report `finish_reason=tool_calls` when Solo emits tools with `stop`, so clients such as MiMo Code do not abort mid-loop.
- Overview no longer carries the usage breakdown and credit deduction history panels; it keeps the gateway status, traffic, account pool, resources and ranking cards.
- The credit expiry distribution moved from Overview to Usage, where it sits under the Stats tab and follows the same time window as the tables above it.
- Overview and Usage both default to the "Today" window; Usage also offers Today on its Quota tab, which previously started at the full retained range.

### 中文

- Trae 默认保留调用方系统提示词，转发 `response_format`，并在 Solo 带着工具调用却返回 `stop` 时改报 `finish_reason=tool_calls`，避免 MiMo Code 等客户端中途停掉。
- 概览移除「用量明细」与「积分扣除历史」，只保留网关状态、流量、账号池、资源与排行卡片。
- 「积分到期分布」从概览移到用量页，位于「统计」标签页内，并跟随上方表格的时间窗口。
- 概览与用量页的时间查询都默认「今天」；用量页的「额度」标签页也新增「今天」，此前默认是全部留存范围。

## 0.7.13 - 2026-10-27

### English

- Access page: the Show raw API control sits beside the Native API title (not far right), and uses a secondary button when collapsed so it is easier to find.
- The display catalog now reloads in the background every 10 minutes by default instead of only when a client asks, so a model an upstream adds appears in `/v1/models` without a manual refresh. Set `CLI2API_CATALOG_REFRESH_INTERVAL` (Go duration such as `15m`, or plain minutes) to change the interval; `0` disables the loop.
- The last successful catalog is persisted in SQLite and restored on boot, and a failed catalog refresh now keeps serving the previous model list instead of returning an empty one.
- `GET /v1/models` reports `catalog.synced_at` and `catalog.stale` beside `data`; upstream error text stays on the console endpoint so a scoped API key cannot read another provider's failure.
- `GET /api/model-catalog` (console) returns the catalog sync state with per-provider last success time, last error, and model count; `POST /api/model-catalog` forces a refresh.
- Codex streaming keeps the function name on tool calls: it is read from `response.output_item.added` instead of the argument events, which carry only the item id. Clients no longer receive a tool call whose `name` is empty, and a session that recorded one no longer has every later request rejected with `Invalid 'input[n].name': empty string`.
- Add opt-in earliest-expiry-first (FEFO) account routing. It ranks accounts only inside the same provider family by their nearest spendable credit-package expiry, rotates accounts expiring on the same UTC+8 day, and falls back to round-robin when a provider has no package dates. Existing round-robin, weighted round-robin, fill-first, affinity, pinning, region/provider gates, cooldowns, and failover are unchanged.
- Refresh a FEFO account's quota after each completed response or stream (coalesced per account) and re-probe accounts whose trusted packages all expired, so newly granted or spent credits change the next selection without waiting for a manual refresh.
- Expose WorkBuddy package expiry as FEFO-eligible only for personal identities with a complete, paginated result. Enterprise/shared identities, full result pages, undated packages, and other providers' reset windows or token lifetimes never invent a priority.
- Add the FEFO option to System → Account routing strategy and document that Qoder, Trae, Devin, Command Code, and Codex do not currently provide per-package credit expiry, so their accounts stay in the unknown-date cohort.
- Add the China-region Kimi Code provider: sign in with the official device-code flow (or import the credential the Kimi Code client already stored), discover the subscription's real model list with its context window, thinking efforts, vision and tool support, and serve `/v1/chat/completions`, `/v1/responses` and `/v1/messages` from it.
- Kimi Code accounts show the four subscription usage windows (5-hour, 7-day, monthly, monthly-code) in the console, refresh their access token automatically, and can be routed alongside every other provider in the account pool.
- Kimi Code models accept a console reasoning default on the Providers page; a request that does not specify a level uses it, then the model's own default.
- Kimi Code is import-only: import the credential the client already stored, but export is not offered, because the credential is a bearer token pair and exporting it would hand out the account.

### 中文

- Access 页：「展开原始 API」按钮紧挨「原生 API」标题（不再靠右），收起态使用 secondary 按钮样式，更容易发现。
- 模型目录改为后台自动刷新，默认每 10 分钟一次，不再只在客户端请求时才拉取，上游新增的模型无需手动刷新就会出现在 `/v1/models`。刷新间隔用 `CLI2API_CATALOG_REFRESH_INTERVAL` 调整（Go duration 如 `15m`，或纯数字表示分钟），设为 `0` 关闭定时刷新。
- 最近一次成功的模型目录会写入 SQLite 并在启动时恢复；目录刷新失败时继续使用上一次的有效列表，不再返回空列表。
- `GET /v1/models` 在 `data` 旁边新增 `catalog.synced_at` 与 `catalog.stale`；上游错误文本只留在控制台接口，限定供应商的 API Key 读不到其他供应商的失败原因。
- 控制台 `GET /api/model-catalog` 返回目录同步状态（每个供应商的上次成功时间、失败原因、模型数量），`POST /api/model-catalog` 立即刷新一次。
- Codex 流式工具调用保留函数名：名字改从 `response.output_item.added` 的 item 读取，参数事件本身只带 item id。客户端不会再收到 `name` 为空的工具调用，之前记录过这种调用的会话也不会再被 `Invalid 'input[n].name': empty string` 整轮拒绝。
- 新增可选的同供应商最早到期优先（FEFO）调度：只在同一供应商内按最近可消费积分包到期日排序，同一 UTC+8 自然日到期的账号公平轮询；供应商没有积分包日期时回退轮询。原有轮询、加权轮询、顺序填满、会话粘性、固定账号、地域/供应商限制、冷却与故障切换保持不变。
- 每次响应或流结束后异步刷新该账号额度（按账号合并请求），并重新探测积分包已全部过期的账号，让新增或已消耗的积分立即影响下一次选择，无需手动刷新。
- 仅 WorkBuddy 个人身份且分页结果完整的积分包参与 FEFO 优先级；企业/共享身份、满 100 条的未确认列表、无日期包，以及其他供应商的重置窗口或令牌有效期都不会被伪造成到期优先级。
- 在「系统 → 账号调度策略」增加 FEFO 选项，并说明 Qoder、Trae、Devin、Command Code、Codex 当前不提供逐包积分到期信息，其账号保持未知日期分组。
- 新增中国区 Kimi Code Provider：用官方设备码授权登录（也可导入 Kimi Code 客户端已保存的凭证），自动发现该订阅真实可用的模型，以及每个模型的上下文、Thinking 档位、视觉和工具支持，并通过它提供 `/v1/chat/completions`、`/v1/responses` 和 `/v1/messages`。
- Kimi Code 账号会在控制台显示四个订阅额度窗口（5 小时、7 天、月度、月度 Code），自动刷新访问令牌，并和其他供应商一样进入账号池参与调度。
- Kimi Code 模型可在「供应商」页设置控制台默认思考强度：请求未指定档位时先用它，再用模型自身的默认档位。
- Kimi Code 只支持导入、不支持导出：可以导入客户端已保存的凭证，但控制台不提供导出，因为凭证是一对 bearer token，导出等于把账号交出去。

## 0.7.12 - 2026-10-25

### English

- Accounts batch import opens the file picker on mobile again (Safari no longer ignores the hidden-input click), and the confirm sheet fits small screens.

### 中文

- 账号页批量导入在移动端可再次打开文件选择器（Safari 不再忽略隐藏 input 的 click），确认弹层也适配小屏。

## 0.7.11 - 2026-10-25

### English

- Accounts page: batch-import WorkBuddy hub packs with auto-detect (format / provider / region) and a confirm step to choose WorkBuddy CN or Global; other providers are detected but not imported yet. Reuses the existing import API.

### 中文

- 账号页批量导入：自动识别凭证 format / 供应商 / 区，确认后可选择 WorkBuddy 国内版或国际版导入；其它供应商仅识别、暂不导入。复用现有导入接口。

## 0.7.10 - 2026-10-25

### English

- Access chat polish: collapsed thinking pill with spinner/shimmer, grouped tool calls, compact composer selects (menus open upward), message foot with copy + credits.
- Access chat: catalog credits stay on the model picker; per-turn foot notes show tokens + credits only when the stream reports `usage.credits` / `consumed_credits`.
- Access page: multi-turn chat stage with AffectUI-like composer (attach + compact account/model + round send in one pad); thinking / tool-call cards use surface hierarchy; Raw API and endpoints drop heavy borders. Dev preview via `?demo=1` or Preview sample.
- Usage Cost tab leads with quota and tokens for every model; DeepSeek list-price CNY is optional and collapsed by default.
- Fix WorkBuddy credential import camelCase decode; accept hub `workbuddy-accounts` export packs and import multiple accounts in one paste (partial success with success/fail summary).

### 中文

- 接入页对话抛光：思考默认一行 pill（spinner/shimmer）、工具分组折叠、Composer 控件更紧（菜单上弹）、气泡脚注复制 + credits。
- 接入页对话：选型继续用目录 credits；本轮脚注仅在流式 `usage.credits` / `consumed_credits` 有值时显示 token + 消耗。
- 接入页：多轮对话主舞台，Composer 一体卡（附件 + 紧凑账号/模型 + 圆形发送）；思考/工具卡靠表面层级、少描边；原始 API 与端点卡去硬边框。开发预览：`?demo=1` 或「预览样例」。
- 用量「额度」页以各模型的订阅额度与 token 为主；DeepSeek 官方列表价折算改为可选，默认折叠。
- 修好 WorkBuddy 凭证 camelCase 解码；支持 hub `workbuddy-accounts` 整包粘贴批量导入（尽量导入成功项，汇总成功/失败）。

## 0.7.9 - 2026-10-24

### English

- Request-log retention is configurable via `CLI2API_LOG_RETENTION_DAYS` (default 7) and `CLI2API_LOG_MAX_ROWS` (default 20000); invalid values fall back so pruning cannot be disabled by typo. Based on external contribution [#336](https://github.com/caigee-cmd/cli2api/pull/336) by [@YunMo-yanyu](https://github.com/YunMo-yanyu).
- Extend the existing **Usage** console page with a **Cost** tab: quota consumed across providers, DeepSeek list-price equivalent cost (peak / off-peak), stacked cost-over-time chart, per-model breakdown, and the official price table. Stats tab (requests / latency / tokens / credits by model · account · provider) is unchanged. Adds `deepseek-flash` to the pricing catalog. Credits reuse the `#361` `request_usage_details` fallback so numbers match the Stats tab. Based on external contribution [#336](https://github.com/caigee-cmd/cli2api/pull/336) by [@YunMo-yanyu](https://github.com/YunMo-yanyu).
- WorkBuddy streaming requests now fail over when the first response byte never arrives within `WORKBUDDY_STREAM_TTFB_MS` (aligned with Qoder’s first-byte timeout), without dropping the existing 401 token-refresh path. Based on external contribution [#336](https://github.com/caigee-cmd/cli2api/pull/336) by [@YunMo-yanyu](https://github.com/YunMo-yanyu).

### 中文

- 请求日志保留可通过 `CLI2API_LOG_RETENTION_DAYS`（默认 7 天）与 `CLI2API_LOG_MAX_ROWS`（默认 20000 行）配置；非法值回退默认，避免误配关掉清理。基于外部贡献 [#336](https://github.com/caigee-cmd/cli2api/pull/336)（[@YunMo-yanyu](https://github.com/YunMo-yanyu)）。
- 在现有「用量」页增加 **费用** 选项卡：跨供应商订阅额度、DeepSeek 官方列表价折合费用（高峰/闲时）、费用趋势图、按模型明细与官方价格表。统计选项卡（按模型·账号·供应商的请求/延迟/token/额度）保持不变。价格目录补上 `deepseek-flash`。额度汇总沿用 #361 的 `request_usage_details` 回退，与统计页数字一致。基于外部贡献 [#336](https://github.com/caigee-cmd/cli2api/pull/336)（[@YunMo-yanyu](https://github.com/YunMo-yanyu)）。
- WorkBuddy 流式请求在 `WORKBUDDY_STREAM_TTFB_MS` 内收不到首字节时会切换账号（对齐 Qoder 首字节超时），并保留原有 401 刷新 token 路径。基于外部贡献 [#336](https://github.com/caigee-cmd/cli2api/pull/336)（[@YunMo-yanyu](https://github.com/YunMo-yanyu)）。

## 0.7.8 - 2026-10-24

### English

- Access page: rename nav to Access & try / 接入与调试, and split into Chat (single-turn streaming) and Native API (playground + curl) tabs with shared account, model, and Base URL.
- Providers catalog: keep「自定义」reasoning controls from stretching desktop table columns, and stack label + select on mobile cards.
- Align Qoder Cosy fingerprint with hub v1.7.3: CLI 1.1.75, desktop Cosy-Version 0.4.8, undici User-Agent, business/scene/clientip headers, lowercase `agree` data-policy; machineos on all COSY requests, hostname SSE-only.
- Bump Codex outbound cloaking User-Agent to official stable `0.164.0`.
- Pass through inbound `X-Codex-Turn-Metadata`, `X-Codex-Turn-State`, and `X-Codex-Window-Id` on Codex OAuth (non-SIWC) upstream requests, matching CLIProxyAPI’s narrow EnsureHeader set.

### 中文

- 接入页：侧栏改名为「接入与调试」，拆成「对话」（单轮流式）与「原生 API」（调试台 + curl）两个页签，账号、模型和 Base URL 共用。
- 模型目录：自定义推理强度控件不再撑宽桌面表格列；移动端卡片改为标签在上、选择器独占一行。
- Qoder Cosy 指纹对齐 hub v1.7.3：CLI 1.1.75、桌面签到 Cosy-Version 0.4.8、undici User-Agent、business/scene/clientip 头、data-policy 小写 `agree`；machineos 全 COSY 请求携带，hostname 仅 SSE。
- Codex 出站伪装 User-Agent 升至官方稳定版 `0.164.0`。
- Codex OAuth（非 SIWC）上游透传入站 `X-Codex-Turn-Metadata` / `X-Codex-Turn-State` / `X-Codex-Window-Id`，范围与 CLIProxyAPI EnsureHeader 一致、不扩大。

## 0.7.7 - 2026-10-24

### English

- Model catalogs now expose the image-input capability each provider already reports (`WorkBuddy.supportsImages`, QwenWork `is_vl`, Devin and Codex modalities) as `supports_images` on `/v1/models` and `/api/models`. The field appears only when the provider declares support; a route merged across regions keeps it only when every region declares it; Qoder mirrors the worker catalog's `is_vl` when present.
- The documented image boundary now matches the upstream catalogs: WorkBuddy's DeepSeek V4 / V4.1, GLM, Kimi, and other `supportsImages` models accept images through the gateway, while Trae, Command, and Zhipu do not declare image input.
- Overview and Usage get looser mobile density: stacked label/value pairs, card lists for dense tables on small screens, and stronger horizontal scroll on desktop tables—without heavier borders.
- When a new release is available, the sidebar update entry keeps its accent styling and adds a light GSAP left-right nudge every few seconds (skipped under prefers-reduced-motion).

### 中文

- 模型目录新增 `supports_images`：把各 Provider 已解析到的图片能力（WorkBuddy `supportsImages`、QwenWork `is_vl`、Devin 与 Codex 的模态声明）透出到 `/v1/models` 与 `/api/models`。只有上游明确声明时才出现；多地域合并的路由要求每个地域都声明；Qoder 在 worker 目录返回 `is_vl` 时同步透出。
- 文档里的图片能力边界与上游目录对齐：WorkBuddy 的 DeepSeek V4 / V4.1、GLM、Kimi 等 `supportsImages` 模型可以通过网关识图；Trae、Command、Zhipu 未声明图片输入。
- Overview / Usage 小屏疏密度：标签与数值上下排、密集表格改卡片列表，桌面加强横向滚动；不加粗边框。
- 有新版本时，侧栏左下角更新入口保留 accent，并用很轻的 GSAP 左右晃一下（每隔几秒；减动效时只留颜色/圆点）。

## 0.7.6 - 2026-10-24

### English

- Codex outer / CLI OAuth now proactively refreshes with a 96-hour lead (aligned with CLIProxyAPI `CodexOuterRefreshLead`), while Sign in with ChatGPT (SIWC) keeps the short 5-minute lead—so the nominal 10-day JWT is renewed before early upstream invalidation without changing SIWC behavior.
- Codex model catalog adds `gpt-6.1-sol` (GPT-6.1-Sol) from the upstream client model list, with the same slim fields as other `gpt-*` entries.
- Add QwenWork CN and Global daily check-in: account cards now expose the manual action and the off-by-default automatic switch, with per-account and provider default times shared with the existing scheduler.
- QwenWork check-in tries the desktop client's `daily-check-in` claim API first, then falls back to today's server-granted free credits: CN reads the daily wallet balance, Global reports the gateway gap (claim route 404) instead of a fake success.

### 中文

- Codex outer / CLI OAuth 主动刷新提前量对齐 CLIProxyAPI 的 `CodexOuterRefreshLead`（96 小时），Sign in with ChatGPT（SIWC）仍保持 5 分钟短 lead，以便在名义 10 天 JWT 被上游提前作废前续期，且不改变 SIWC 行为。
- Codex 模型目录新增上游客户端列表中的 `gpt-6.1-sol`（GPT-6.1-Sol），字段结构与其它 `gpt-*` 条目一致。
- 新增 QwenWork 国内版/国际版每日签到：账号卡片提供手动签到和默认关闭的自动签到开关，沿用现有的账号级/供应商级签到时间调度。
- QwenWork 签到先调用桌面客户端的每日签到领取接口，接口未开放时改为核对当日服务端发放的免费积分：国内版读取钱包日额度，国际版明确记录网关未开放的原因，不伪造成功。

## 0.7.5 - 2026-10-23

### English

- Scheduled check-in and WorkBuddy keepalive now rate-limit across accounts: each 30s maintenance tick handles at most 2 due accounts, with ~2s + small FNV jitter between them, so a large AutoCheckin fleet no longer bursts upstream in one tick after the configured time (or after 22:00 for keepalive).

### 中文

- 定时签到与 WorkBuddy Keepalive 现按跨账号限速：每个 30 秒 maintenance tick 最多处理 2 个到期账号，账号之间约 2 秒并加小幅 FNV jitter，避免大量 AutoCheckin 账号在配置时刻（或 Keepalive 的 22:00）后同一 tick 背靠背打上游。

## 0.7.4 - 2026-10-23

### English

- Non-lite Codex requests that include tools now preserve an explicit `parallel_tool_calls: false` from the caller instead of forcing `true` (aligned with CLIProxyAPI). Absent values still default to `true`.
- Codex credentials that enable `use_siwc` now keep both the outer ChatGPT OAuth and SIWC partitions. Requests whose User-Agent identifies an official Codex client (`codex-desktop`, `Codex Desktop`, `codex-rs`, `codex_cli_rs`, `codex-tui`, `codex_exec`) ignore `use_siwc`, use the outer token, and fail closed with 401 when the outer `access_token` is missing; refresh on that path only updates the outer partition.
- Overview now shows a credit-expiry distribution: credits grouped by the calendar day they lapse, one colour per account, plus a soonest-expiry table (numeric columns centred) with the daily burn rate needed to spend each batch before it expires (FEFO). Already-expired packages that still hold remain appear in a dedicated bucket.
- Overview adds a usage breakdown table with token split (prompt / completion / total), prefix-cache hit rate, error count and latency, switchable between account / model / realm dimensions and sortable by total tokens, requests, errors or latency, following the selected window (1h / 24h / 7d / 30d / custom). The 30s / visibility refresh quietly reloads this panel with the traffic chart.
- Overview adds a credit deduction history that sums upstream `usage.credit` per account and per model, with KPI tiles for credits deducted, matched tokens, average credits per 1M tokens, valid samples and cache hit rate; requests without a credit reading are excluded and nothing is back-filled, model rates come from the regional price catalog. Credit SQL shares one nullable expression with the #361 zero-coalesced form.
- Overview time switcher gains a **Today** option (calendar day since local midnight → now), placed before 1h.
- Overview now defaults to the Today window on load (was 24h).
- WorkBuddy payload sanitization now rewrites DeepSeek Harness identity sentences and the DSH Web GUI paragraph (and reduces the `DeepSeek Harness` brand prose) on system / developer / assistant messages, matching upstream 11-128 channel-fingerprint fixes. User and tool messages stay untouched.
- WorkBuddy chat no longer treats every post-401 token refresh failure as `login_required`: only real session-dead / auth refresh failures keep that path; transport and 5xx refresh errors stay `unavailable` with a short pool cooldown and a diagnostic Observe entry.

### 中文

- 非 lite 且带 tools 的 Codex 请求会保留调用方显式传入的 `parallel_tool_calls: false`，不再强制改成 `true`（与 CLIProxyAPI 对齐）；未传时仍默认 `true`。
- 启用 `use_siwc` 的 Codex 凭据会同时保留 outer ChatGPT OAuth 与 SIWC 分区。入站 User-Agent 识别为官方 Codex 客户端（`codex-desktop` / `Codex Desktop` / `codex-rs` / `codex_cli_rs` / `codex-tui` / `codex_exec`）时忽略 `use_siwc`、改用 outer token；缺少 outer `access_token` 则直接 401，且该路径下的刷新只更新 outer。
- 概览新增「积分到期分布」：按自然日聚合各账号即将失效的积分，颜色区分账号；并附最近到期提醒表（数字列居中），给出该日到期积分与到期前需要的日均消耗（FEFO）。过期但仍有余额的包单独成桶。
- 概览新增「用量明细」：按维度展示 token 构成（Prompt / Completion / 合计）、前缀缓存命中率、失败数与延迟，可在账号 / 模型 / 域之间切换，并支持按合计 Token、请求数、失败数、平均延迟排序，跟随上方所选时间窗口（1 小时 / 24 小时 / 7 天 / 30 天 / 自定义）。30 秒 / 切回可见时与流量图一并 quiet 刷新。
- 概览新增「积分扣除历史」：按账号与模型汇总上游 `usage.credit`，顶部 KPI 展示扣除积分、匹配 Token、平均积分 / 1M Token、有效样本与缓存命中率；未观测到积分的请求不计入、不做任何回填，模型倍率取自区域价格目录。积分 SQL 与 #361 收敛为可空 / 补零两种包装。
- 概览页顶部时间切换新增「今天」选项（按本地零点到当前的日历日统计），排在「1小时」之前。
- 概览页默认时间窗口改为「今天」（原为 24 小时）。
- WorkBuddy 出站清洗对齐上游 11-128 渠道指纹修复：对 system / developer / assistant 改写 DeepSeek Harness 自我介绍句与 DSH Web GUI 描述段，并将散文中的 `DeepSeek Harness` 缩为 `DeepSeek`；user / tool 消息保持原样。
- WorkBuddy 聊天在 401 后刷新 token 失败时不再一律标成 `login_required`：仅真实 session-dead / auth 刷新失败保留该路径；运输错误与 5xx 刷新失败保持 `unavailable`，走池短冷却，并用现有 Observe 记下可诊断原因。

## 0.7.3 - 2026-10-23

### English

- Current concurrency now tracks live proxy requests (Acquire/Release on the executor). `/api/live` and the sidebar update within a few seconds; health probes no longer overwrite in-flight counts.

### 中文

- 「当前并发」改为按代理正在处理的请求实时加减；`/api/live` 与侧边栏几秒内跟上，健康探活不再覆盖进行中的并发数。

## 0.7.2 - 2026-10-22

### English

- **New** · Overview stats: request outcome mix (ok / incomplete / error / canceled), cache hit rate, streaming share and canceled requests; the account pool lists cooling accounts with reason, countdown and backoff level, plus the session-affinity hit rate.
- **New** · Overview drill-down: click a model, account, provider or error kind, or drag a range on the traffic chart, to open the matching logs. Time windows add 30d and custom; stats auto-refresh every 30s and can be paused.
- **New** · Usage page with sortable tables by model, account and provider (requests, success rate, p95, tokens, cache, credits). The traffic chart switches between requests, tokens and latency.
- **New** · Live concurrency: an overview strip (in-flight vs. capacity, last 60s), per-account in-flight bars and a sidebar indicator.
- **Console** · Roomier sidebar; denser overview without decorative icons; the Accounts page header is one toolbar (status, provider, search, actions).
- **Console** · Calmer styling: one small-text size, smaller radii and shadows, neutral status boxes; pages show content without skeleton fades or entrance animations.
- **Console** · Edit-account modal is wider; the model picker adds search, select all / clear, and flags saved models that left the catalog.
- **API** · New `GET /api/live` for live concurrency. `/api/logs/stats` accepts `dim=model,account,provider` and `limit`, with p95, token, cache and credit totals; `/api/logs/requests` accepts `provider`. Existing calls behave as before.

### 中文

- **新功能** · 概览统计：请求结果构成（成功 / 未完成 / 失败 / 取消）、缓存命中率、流式占比与取消数；账号池列出冷却账号的原因、倒计时与退避等级，并显示会话亲和命中率。
- **新功能** · 概览下钻：点击模型、账号、供应商或错误类型，或在流量图上拖选时间段，直接打开对应日志。时间窗口新增 30 天与自定义；统计每 30 秒自动刷新，可暂停。
- **新功能** · 新增「用量」页，按模型、账号、供应商展示可排序表格（请求数、成功率、p95、Tokens、缓存、积分）；流量图可切换请求 / Tokens / 延迟。
- **新功能** · 实时并发：概览顶部并发条（当前并发 / 总容量、近 60 秒）、每个账号的并发细条，以及侧边栏并发指示。
- **界面** · 侧边栏更宽松；概览更紧凑，去掉装饰图标；账号页顶部合并为一行工具栏（状态、供应商、搜索、操作）。
- **界面** · 样式更克制：小字统一字号，圆角与阴影收小，状态提示改用中性底色；页面加载后直接显示内容，不再有骨架屏淡入和入场动画。
- **界面** · 编辑账号弹窗加宽；模型选择支持搜索、全选 / 清空，并标出已下线的模型。
- **API** · 新增 `GET /api/live` 实时并发接口；`/api/logs/stats` 支持 `dim=model,account,provider` 与 `limit`，返回 p95、Token、缓存与积分合计；`/api/logs/requests` 支持 `provider` 筛选。原有调用行为不变。

## 0.7.1 - 2026-10-22

### English

- **New** · Qoder: optional per-account traffic guard, off by default (`QODER_TRAFFIC_GUARD=on`; `QODER_TRAFFIC_GUARD_CONCURRENCY`, `QODER_TRAFFIC_GUARD_INTERVAL_MS`).
- **New** · WorkBuddy: `X-Private-Data` is configurable via `WORKBUDDY_PRIVATE_DATA=auto|true|false` (default `auto`).
- **New** · Qoder requests resolve the model name / display name to the account's catalog key; unknown models return `model_not_found`.
- **Fix** · `/v1/messages` accepts in-band `role: "system"` messages; Claude Code 2.1.293+ no longer gets 400. (#328)
- **Fix** · Codex Responses-Lite models (e.g. GPT-5.6) send `reasoning.context: "all_turns"`, fixing upstream 400. (#341)
- **Fix** · WorkBuddy keeps `max` reasoning effort when the catalog advertises it, instead of downgrading to `xhigh`. (#343)
- **Fix** · Qoder streams: unique completion `id` per request, finish on `[DONE]`, release the upstream on early close, and fail over heartbeat-only upstreams (`upstream_empty`). (#339, #342, #345)
- **Fix** · Qoder tool calls: non-streaming keeps the full call; an empty `tool_calls` array is ignored. (#338)
- **Fix** · Qoder keeps earlier assistant `reasoning_content`; model key mappings refresh with each catalog refresh. (#340, #346)
- **Fix** · Legacy Qoder accounts (pre-v0.7.0 CLI login, no PAT) fall back to the worker again; no more "account not found". (#337)
- **Fix** · Qoder check-in shows why nothing was claimable; token renewal no longer clears held tokens. (#327)
- **Fix** · A Qoder worker that crashes right after an automatic restart is now restarted again instead of staying dead.
- **Console** · Cleaner look: fewer nested borders, one type scale with the system UI font, larger sidebar text; API key prefix on its own line. (#347, #350)
- **Console** · Settings split into tabs (General, Routing, Security & data, Updates); the sidebar version entry opens release notes and update actions. (#347)
- **Upstream** · Qoder: one client version (1.1.32) across headers, signature and User-Agent (`QoderWork/…` CN, `Qoder/…` intl); streams add `cosy-machineos` / `cosy-machinehostname`. (#349)
- **Upstream** · WorkBuddy client versions: CLI 2.161.0, desktop 5.8.4 / 5.8.3 (CN), IDE 2.168.2, three-part desktop User-Agent. (#349)
- **Upstream** · WorkBuddy: chat 401 refreshes the token once and retries; TLS verification errors are no longer retried or failed over. (#348)
- **Upstream** · Qoder: 10605 queue errors return 429 + `Retry-After` without cooling the account; HTTP 430 shows "client version too low". (#348)
- **Upstream** · Devin: daily vs. weekly quota told apart; cooldown follows the window reset and lifts early once usage recovers. (#348)

### 中文

- **新功能** · Qoder：新增可选的单账号流量守卫，默认关闭（`QODER_TRAFFIC_GUARD=on`；可调 `QODER_TRAFFIC_GUARD_CONCURRENCY`、`QODER_TRAFFIC_GUARD_INTERVAL_MS`）。
- **新功能** · WorkBuddy：`X-Private-Data` 可通过 `WORKBUDDY_PRIVATE_DATA=auto|true|false` 配置（默认 `auto`）。
- **新功能** · Qoder 按模型目录把模型名/显示名解析为账号网关 key，未知模型返回 `model_not_found`。
- **修复** · `/v1/messages` 支持 `messages` 内的 `role: "system"`，Claude Code 2.1.293+ 不再报 400。（#328）
- **修复** · Codex Responses-Lite 模型（如 GPT-5.6）发送 `reasoning.context: "all_turns"`，修复上游 400。（#341）
- **修复** · WorkBuddy 在目录支持 `max` 时不再把推理档位降级为 `xhigh`。（#343）
- **修复** · Qoder 流式：每请求唯一 completion `id`、收到 `[DONE]` 即结束、提前关闭时释放上游、仅心跳的上游以 `upstream_empty` 切换。（#339、#342、#345）
- **修复** · Qoder 工具调用：非流式保留完整调用；空 `tool_calls` 数组不再视为工具调用。（#338）
- **修复** · Qoder 保留历史 assistant 的 `reasoning_content`；模型 key 映射随目录刷新替换。（#340、#346）
- **修复** · v0.7.0 前 CLI 登录、无 PAT 的老 Qoder 账号重新回退 worker，不再报 "account not found"。（#337）
- **修复** · Qoder 签到无可领活动时显示原因；token 续期缺字段时保留原值。（#327）
- **修复** · Qoder worker 在自动重启后立即再次崩溃时会继续重启，不再一直处于 dead 状态。
- **界面** · 控制台更简洁：去掉嵌套线框、统一字号与系统字体、侧边栏加大；API 密钥前缀单独一行。（#347、#350）
- **界面** · 系统设置分为常规 / 路由调度 / 安全与数据 / 版本更新标签页；侧边栏版本入口可查看更新日志并下载/更新。（#347）
- **上游对齐** · Qoder：请求头、签名、User-Agent 统一客户端版本 1.1.32（国内 `QoderWork/…`、国际 `Qoder/…`）；流式补 `cosy-machineos` / `cosy-machinehostname`。（#349）
- **上游对齐** · WorkBuddy 客户端版本：CLI 2.161.0、桌面 5.8.4 / 5.8.3（国内）、IDE 2.168.2，桌面 UA 改为三段式。（#349）
- **上游对齐** · WorkBuddy：对话 401 先刷新 token 再重试同账号；TLS 证书错误不再重试或换号。（#348）
- **上游对齐** · Qoder：10605 排队返回 429 + `Retry-After`，不冷却账号；HTTP 430 提示“客户端版本过低”。（#348）
- **上游对齐** · Devin：区分日/周额度，按窗口重置时间冷却，额度恢复后提前解除。（#348）

## 0.7.0 - 2026-10-19

### English

- Report prompt-cache token usage for Trae, Qoder, and WorkBuddy, streaming and non-streaming. Thanks [@longlixiao-ong](https://github.com/longlixiao-ong) (#293).
- Streams that end with a `finish_reason` but no `[DONE]` no longer return a 502.
- Qoder PAT accounts now run fully in-process (no per-account Node worker), including token refresh, model catalog, quota, and check-in. (#313)
- Quota headline sums plan + add-on + org packs; reasoning streams again for in-process chat.
- Upgrade: existing Qoder accounts keep the worker path until you re-login them with a PAT.
- New QwenWork provider (`qwenwork/` prefix): browser login, chat on all three APIs with tools, live model list, and quota.
- CN region (千问办公) is not yet verified with a real account; Global is verified.
- Pin a region with `workbuddy-cn/…` or `workbuddy-gl/…` (also Qoder); `-auto/` picks the region automatically.
- Bare `qoder/…`, `workbuddy/…`, and unprefixed models behave as before.
- Accounts marked out of quota are re-checked automatically, with backoff from 5 minutes up to 24 hours.
- When every eligible account is out of quota, requests return `429 insufficient_quota` instead of "no accounts available".
- Point README community discussion at the Telegram group instead of LINUX DO, and add a personal WeChat QR for contact in China.
- Trae browser login now completes on its own, even for slow SMS logins; pasting the callback URL still works as a fallback.
- WorkBuddy strips client fingerprint phrases (e.g. from Claude Code) before sending, avoiding upstream error `11128`.
- Qoder streams with no first byte in 30s now fail over to the next account instead of hanging (`QODER_STREAM_TTFB_MS`).
- Worker aborts a stream only after 300s with no new chunk (`QODER_STREAM_TOTAL_MS`); long active streams are not cut.
- Unresponsive Qoder workers are detected and restarted automatically.

### 中文

- Trae、Qoder、WorkBuddy 流式与非流式均返回 Prompt 缓存 token 用量。感谢 [@longlixiao-ong](https://github.com/longlixiao-ong)（#293）。
- 上游带 `finish_reason` 结束但未发 `[DONE]` 的流不再报 502。
- Qoder PAT 账号改为完全进程内运行（不再每账号起 Node worker），token 刷新、模型目录、额度、签到均在进程内完成。(#313)
- 额度总览合计套餐 + 资源包 + 组织资源包；进程内对话恢复思考流输出。
- 升级提示：老 Qoder 账号需用 PAT 重新登录才会切到进程内模式，否则继续走 worker。
- 新增 QwenWork（千问办公）供应商（`qwenwork/` 前缀）：浏览器登录，三种 API 均可对话并支持工具调用，模型列表与额度实时获取。
- 国内版尚未用真实账号验证；国际版已验证。
- 可用 `workbuddy-cn/…`、`workbuddy-gl/…` 指定区域（Qoder 同理）；`-auto/` 由池自动选区域。
- 裸前缀 `qoder/…`、`workbuddy/…` 及无前缀模型行为不变。
- 额度耗尽的账号会自动复查，间隔从 5 分钟逐步退避到 24 小时。
- 所有可用账号都耗尽额度时，返回 `429 insufficient_quota`，而非「无可用账户」。
- README 的社区讨论入口改为 Telegram 群，不再指向 LINUX DO，并增加国内联系用的个人微信二维码。
- Trae 浏览器登录现在可自动完成，短信登录较慢也不会失败；粘贴回调 URL 仍可兜底。
- WorkBuddy 发送前自动改写客户端特征语句（如 Claude Code 注入的内容），避免上游报 `11128`。
- Qoder 流式请求 30 秒内无首字节会切换到下一个账号，不再挂起（`QODER_STREAM_TTFB_MS`）。
- worker 仅在连续 300 秒无新数据时中止流（`QODER_STREAM_TOTAL_MS`），持续输出的长流不会被截断。
- 无响应的 Qoder worker 会被自动发现并重启。

## 0.6.23 - 2026-10-18

### English

- Codex accounts can Sign in with ChatGPT separately from Codex CLI OAuth, so website logins refresh and chat on the matching endpoints.
- Codex SSE now joins a `data:` field split across lines, so a split `response.completed` still reports usage.
- Codex tool schemas drop `minimum` / `maximum` bounds outside signed 64-bit integers, avoiding upstream HTTP 500s from MCP tools.
- Devin accounts leave quota cooldown early once daily and weekly usage windows show remaining quota again.
- Clear WorkBuddy account and model cooldowns after a successful or already-completed check-in.

### 中文

- Codex 账号可与 CLI OAuth 分开使用 Sign in with ChatGPT，网页登录的凭证会走对应的刷新和聊天端点。
- Codex SSE 会把拆成多行的 `data:` 拼回去，拆开的 `response.completed` 仍能带上用量。
- Codex 工具 schema 会去掉超出有符号 64 位整数的 `minimum` / `maximum`，避免 MCP 工具触发上游 HTTP 500。
- Devin 账号在日/周额度窗口恢复有余量后，会提前结束额度冷却。
- WorkBuddy 签到成功或确认今日已签到后，清除账号级和模型级冷却。

## 0.6.22 - 2026-10-16

### English

- Each account can map a client-facing model name to its own real model, and can limit which catalog models it serves.
- Keep quota-exhausted accounts routable for free models. Qoder prices some catalog models at zero (`limited_time_free` tag or a zero `price_factor`), and requests for them spend no credits, but the routing pool filtered every quota-exceeded account unconditionally — leaving those accounts' free models with no account at all. The pool now records which catalog entries the provider prices at zero alongside the cached model list, and the quota filter exempts a request whose model is on that list. Pricing-unknown models are not assumed free, and the exemption is scoped per model, so exhausted accounts stay out of paid routes.
- Send the catalog's gateway model key on Qoder chat. The model catalog publishes a friendly public ID (`qwen3.8-flash`) alongside a distinct gateway key (`qfmodel`). The worker resolved the public ID through the per-account catalog before sending, but the in-process COSY client filled `model_config.key` with whatever the caller asked for. On an account whose gateway routes only the mapped key, that request was never answered — the gateway held it open and the client waited out its own timeout with no error and no bytes in return. The pool now records the public-ID-to-gateway-key mapping from the catalog, and the chat request carries the mapped key. An account whose catalog reports no distinct key keeps its public spelling.
- Fix Qoder chat rejecting multi-turn tool use with `400 InvalidParameter: messages with role "tool" must be a response to a preceeding message with "tool_calls"`. The upstream gateway only pairs a `role:"tool"` message with the assistant turn ahead of it when that assistant message's `content` is a string; OpenAI clients send `"content": null` for a pure tool-call turn, which left the call unregistered. The in-process client now rewrites a nil content on a tool-call turn to an empty string, which the gateway accepts. Call ids, tool results, and all other content shapes pass through untouched.
- Clear a WorkBuddy account's cooldown immediately after a successful daily check-in so newly restored credits can be routed.

### 中文

- 每个账号可以把自己的真实模型映射成对外名字，也可以限制自己只接选中的目录模型。
- 额度耗尽的账号现在仍可路由到免费模型。Qoder 目录中部分模型单价为零（`limited_time_free` 标签或 `price_factor` 为零），请求不消耗积分，但路由池原先无条件排除所有额度耗尽的账号，导致这些账号的免费模型无账号可用。路由池现在在缓存模型列表的同时记录上游报价为零的模型，额度过滤对命中该名单的请求予以豁免。未报价的模型不视为免费；豁免按请求模型限定，额度耗尽的账号依旧不会出现在付费模型的路由上。
- Qoder 聊天请求现在发送目录里的网关模型键。模型目录同时给出一个易读的公网 ID（`qwen3.8-flash`）和一个不同的网关键（`qfmodel`）。Worker 发送前会用账号目录把公网 ID 解析一遍，但进程内 COSY 客户端此前直接把调用方传入的名字填进 `model_config.key`。在只按映射键路由的账号上，这个请求不会得到任何响应——网关把它挂着，客户端一直等到自己的超时，既没有错误也没有数据。路由池现在记录目录给出的「公网 ID → 网关键」映射，聊天请求携带映射后的键。目录未给出不同键的账号保持公网 ID 原样。
- 修复 Qoder 多轮工具调用被上游以 `400 InvalidParameter: messages with role "tool" must be a response to a preceeding message with "tool_calls"` 拒绝的问题。上游网关只在前一条 assistant 消息的 `content` 为字符串时，才把它携带的 `tool_calls` 登记为可配对；OpenAI 客户端对纯工具调用轮次发送的正是 `"content": null`，导致调用未登记、后续 `role:"tool"` 消息被拒。进程内直连客户端现在会把工具调用轮次的 null content 改写为空字符串（网关接受该形状）；工具调用 id、工具结果与其他内容形状均原样保留。
- WorkBuddy 每日签到成功后立即清除对应账号的冷却，让恢复的积分可以马上参与调度。

## 0.6.21 - 2026-10-13

### English

- Browser login no longer opens the authorization page by itself. The URL is shown first, with buttons to open or copy it.
- Top-align the account card check-in row so the status and the scheduled time stay on the same line when the row wraps.
- Honor Devin rate-limit reset times from the response body, and cool an exhausted usage quota for the whole account instead of until local midnight.

### 中文

- 浏览器登录不再自动打开授权页。地址先展示出来，可再选择打开或复制。
- 账号卡片的签到行改为顶部对齐，换行时状态和计划时间不再错位。
- 采用 Devin 响应正文里的限流重置时间，并把用尽的用量配额冷却到整个账号，而不是冷却到本地午夜。

## 0.6.20 - 2026-10-09

### English

- Fix Qoder chat silently returning an empty, spec-invalid completion when the upstream gateway answers 200 with heartbeat frames only. The in-process client now rejects the empty stream before relaying (so the executor can cool the account down and fail over), salvages streams that were cut before the terminal `finish_reason` with a synthesized `stop` / `tool_calls` terminal chunk, and rejects empty non-stream responses instead of fabricating `stop`. Strict OpenAI-compatible clients no longer fail with `Stream ended without finish_reason`.
- Make Qoder quota refresh failures visible to the account refresh endpoint while keeping account readiness intact, and run quota and model catalog refreshes independently.

### 中文

- 修复 Qoder 上游网关偶发 200 + 仅心跳帧时，网关静默返回"空且不合规"的补全。进程内直连客户端现在会在转发前拒绝空流（执行层可冷却该账号并切换到其他账号重试）；对已输出内容但在终态 `finish_reason` 前被切断的流，合成 `stop` / `tool_calls` 终态帧进行挽救；非流式空响应不再凭空合成 `stop` 而是报错。严格的 OpenAI 兼容客户端（如 pi coding agent）不再出现 `Stream ended without finish_reason`。
- 让 Qoder 额度刷新失败在账号刷新接口中可见，同时保持账号就绪状态不受影响，并让额度与模型目录刷新相互独立执行。

## 0.6.19 - 2026-10-07

### English

- Qoder Global accounts can claim the same daily credit campaign as Qoder CN, including automatic check-in. It stays off until the account turns it on.

### 中文

- Qoder 国际版账号可以领取和国内版相同的每日积分活动，并沿用现有自动签到。账号未开启前不会自动签。

## 0.6.18 - 2026-10-07

### English

- Qoder CN check-in no longer sends a partial machine identity. Without the official runtime-info identity, those headers hid the claimable credit campaign.
- Qoder chat now signs requests in the Go process and calls the gateway directly. Per-account Node workers stay for login, model catalog, and quota, and are no longer on the chat path.

### 中文

- Qoder 国内版签到在没有官方 runtime-info 机器身份时不再发送半套机器头。之前这些头会让可领取的积分活动从列表里消失。
- Qoder 聊天改为在 Go 进程内签名并直连网关。每个账号的 Node worker 仍负责登录、模型目录和配额，不再参与聊天请求。

## 0.6.17 - 2026-10-05

### English

- Move the Overview resources card up beside the account pool, ahead of the traffic rankings.

### 中文

- 概览页的资源占用卡片提前到账号池旁边，排在流量排行之前。

## 0.6.16 - 2026-10-04

### English

- Add a Zhipu GLM account. Paste an API key to proxy the official OpenAI-compatible endpoint, with Coding Plan quota on coding keys.

### 中文

- 新增智谱 GLM 账号。粘贴 API Key 即可反向代理官方 OpenAI 兼容端点，Coding Plan 密钥会显示套餐额度。

## 0.6.15 - 2026-10-02

### English

- Run the Qoder CN check-in fingerprint from an executable directory when the account runtime directory cannot run programs.

### 中文

- 账号运行目录不能执行程序时，Qoder 国内版签到会改到可执行目录读取机器指纹。

## 0.6.14 - 2026-10-02

### English

- Fixed the Add Account dialog: the account-type tiles now vertically center the provider icon, radio dot, label, and info button on one line (previously they sat ~8px low), and the dialog title and subtitle are centered.
- Replaced the Codex account mark with the official OpenAI Codex logo (the cloud/`>_` prompt glyph) instead of the generic OpenAI blossom.
- The overview page now shows a Resources card with live memory (RSS) and CPU usage for the server process and each managed worker, refreshed every 5 seconds, plus a new `GET /api/system/resources` endpoint.
- Bounded several unbounded memory paths: non-stream worker responses are capped at 64 MiB, inbound `/v1` request bodies at 64 MiB and console bodies at 1 MiB, per-worker log line buffering at 256 KiB, and the stats cache now evicts entries. Worker SSE parsing no longer retains every frame and builds large strings incrementally instead of repeated concatenation.
- Qoder CN daily check-in now also sends the desktop machine OS, hostname, and risk fingerprint (`Cosy-MachineType` / `Cosy-MachineCode`) that the campaign service requires. v0.6.13 already sent the machine id headers.
- Fix the Overview traffic chart so the success and failure lines each plot their own counts instead of both tracking total requests, and make the failure line cover every non-success outcome (error, incomplete, canceled) so no traffic disappears from the curve.

### 中文

- 修复「添加账号」弹窗对齐：账号类型卡片内的提供方图标、单选圆点、文字和提示按钮现在垂直居中对齐（此前整体偏下约 8px），弹窗标题与副标题改为居中。
- Codex 账号图标换成官方 OpenAI Codex logo（云朵/`>_` 提示符），不再用通用的 OpenAI 花朵标。
- 概览页新增「资源占用」卡片，每 5 秒刷新展示主进程和各 worker 进程的内存（RSS）与 CPU 占用，并新增 `GET /api/system/resources` 接口。
- 收紧了多处内存上限：非流式 worker 响应限制 64 MiB，`/v1` 入站请求体限制 64 MiB、console 请求体限制 1 MiB，单条 worker 日志行缓冲限制 256 KiB，统计缓存加入淘汰；worker SSE 解析不再保留全部帧，大字符串改为增量拼接。
- Qoder 国内版每日签到在 v0.6.13 已发送的机器身份请求头之外，补上活动服务要求的桌面端机器系统、主机名和风控指纹（`Cosy-MachineType` / `Cosy-MachineCode`）。
- 修复概览页流量折线图：成功与失败各自显示自己的计数，不再都跟着总请求数；失败折线覆盖全部未成功请求（失败、未完成、取消），曲线不再丢数据。

## 0.6.13 - 2026-09-29

### English

- Add a system setting to let disabled accounts continue their opted-in automatic daily check-in without making them eligible for chat routing.
- Fix Qoder CN daily check-in by sending the desktop machine fingerprint headers required by the campaign service.
- A routing region latch no longer survives a large change in pool composition. Adding or migrating many accounts of another region now re-seats the route on the largest region instead of leaving the new accounts idle.

### 中文

- 新增系统设置，允许停用账号继续执行已开启的自动签到，同时不会让它们重新参与聊天调度。
- 修复 Qoder 国内版每日签到，补齐活动服务要求的桌面端机器指纹请求头。
- 路由的区域锁定不再跨越大规模的账号池变化。新增或迁移大量其他区域的账号后，路由会重新落到账号最多的区域，而不会让新账号闲置。

## 0.6.12 - 2026-09-29

### English

- The add-account type cards now show only the icon and title. Login details appear when you hover the info button.
- Codex account cards now read the current usage response, so the 5-hour and weekly windows show up instead of staying on "Getting quota…". The subscription tier from that response is shown above the windows.
- A stream that already finished is logged as success when the client disconnects afterward. A disconnect before the stream finishes is still canceled.

### 中文

- 添加账号的类型卡片现在只显示图标和标题。登录方式等说明放到信息按钮上，悬停后再展开。
- Codex 账号卡片现在按当前额度响应读取窗口，5 小时和每周额度会显示出来，不再一直停在「额度获取中…」。响应里的订阅等级会显示在额度窗口上方。
- 流已经结束后客户端再断开，请求日志记为成功。流还没结束就断开，仍然记为取消。

## 0.6.10 - 2026-09-26

### English

- Codex free accounts no longer stay on login failed after a successful sign-in. The console now reads the ChatGPT account id from the login token, and an account that already signed in recovers on the next refresh.

### 中文

- Codex 免费账号登录成功后不再一直显示登录失败。控制台会从登录令牌里读出 ChatGPT 账号 ID，已经登录过的账号会在下次刷新时自动恢复。

## 0.6.9 - 2026-09-25

### English

- Choose an account type from cards instead of a dropdown, with a Codex mark and a scrollable list that keeps the dialog width stable.

### 中文

- 新建账号改为卡片选择渠道，补上 Codex 图标；渠道过多时列表在固定高度内滚动，弹窗保持加宽后的宽度。

## 0.6.8 - 2026-09-25

### English

- Show Codex 5-hour and weekly quota from the account usage probe, and keep a signed-in account on “getting quota” until a real window arrives.

### 中文

- Codex 账号改为从额度探测读取 5 小时和每周窗口；已登录但还没有窗口时显示「额度获取中」，不再显示「额度不可用」或 0/0。

## 0.6.7 - 2026-09-25

### English

- Preserve OpenAI Responses items through Codex native forwarding, while applying the ChatGPT Codex request constraints used by CLIProxyAPI. Chat Completions and Messages requests translated to Codex use the same constraints.
- Add `codex` provider: OpenAI Codex accounts backed by a ChatGPT subscription, with browser OAuth login, automatic token refresh, streaming chat, model catalog, and rate-limit window display.
- Add an experimental **Command Code** (`commandcode.ai`) provider that speaks the CLI's own `/alpha/generate` protocol. That endpoint is not plan-gated, so **every plan works — including the $1 Go plan**, whose Pro-gated `/provider/v1/*` generation endpoints return `403 upgrade_required`. The adapter serves the whole model catalog. Auth is a single `user_…` key, pasted through the existing PAT tab; the model list is read from the anonymous `/provider/v1/models`, and usage is shown from `/alpha/billing/credits` (monthly plan window plus the rolling 5-hour and weekly windows). Streaming is rewritten from the upstream newline-delimited JSON into OpenAI SSE. Experimental: `/alpha/generate` is unpublished and version-coupled, so the pinned CLI version fails loudly rather than degrading silently, and the provider is not production-ready.
- Present Qoder's context window like Trae's Max-context switch. Qoder reports a default window and a larger selectable window per model but has no upstream toggle, so the console now shows the same "default → larger + switch" control it uses for Trae instead of a raw number input. Turning it on sends the model's largest window as the request's `context_length`; turning it off clears the override so requests fall back to the model's default. The window values come from Qoder's own catalog (`default_context_window` / `available_context_windows`) rather than a hardcoded 180000, so glm-5.3-flash shows 1M instead of being capped at 180k. Qoder Global and CN.
- Show Qoder model pricing on the account's model list. Qoder reports a per-model `price_factor` (the multiplier its own client renders as `0.50x Credit`) plus `is_free`/`tags`, but the worker dropped them, so the console showed no price for any Qoder account. The worker now forwards them and the adapter renders the multiplier as the console's credits text. The free badge follows the Qoder client's own rule — the `limited_time_free` tag (or a zero factor) is free, a positive factor is priced — so a model that reports `is_free` alongside a real multiplier (Qwen3.8-Max: `is_free` + `0.5`) shows its multiplier instead of the contradictory "免费 / x0.5" pair. Applies to Qoder Global and Qoder CN.
- Qoder accounts now show their real remaining credits: the account card's headline sums the plan quota, the add-on pack, and the organization resource package instead of reading only the plan quota. Daily check-in rewards are credited to the add-on pack upstream, so before this a plan-only account's card never moved after a check-in (it stayed at e.g. 300 while the reward sat unshown). This matches how the Qoder client itself totals the buckets.

### 中文

- Codex 原生转发保留 OpenAI Responses 的 item，同时按 CLIProxyAPI 的方式改写 ChatGPT Codex 不接受的请求字段。Chat Completions 与 Messages 转成 Codex 请求时使用同一套约束。
- 新增 `codex` provider：基于 ChatGPT 订阅的 OpenAI Codex 账号，支持浏览器 OAuth 登录、自动刷新 token、流式聊天、模型目录和限速窗口展示。
- 新增实验性 **Command Code**（`commandcode.ai`）渠道，走 CLI 自身的 `/alpha/generate` 协议。该端点不受档位限制，**所有套餐均可用 —— 包括 $1 Go 套餐**（其 Pro 专属的 `/provider/v1/*` 生成端点会返回 `403 upgrade_required`）。适配层覆盖整个模型目录。认证只需一把 `user_…` 密钥，经现有 PAT 页签粘贴；模型列表读取匿名的 `/provider/v1/models`；用量从 `/alpha/billing/credits` 展示（月套餐窗口，以及滚动的 5 小时 / 周窗口）。流式响应由上游的换行分隔 JSON 改写为 OpenAI SSE。实验性：`/alpha/generate` 未公开且与 CLI 版本耦合，故钉版不匹配时明确报错而非静默降级，不承诺生产可用。
- Qoder 的上下文窗口改为按 Trae 的「更大上下文」开关呈现。Qoder 每个模型都会上报默认窗口和一个可选更大窗口，但上游没有开关字段，因此控制台不再用裸数字输入框，而是复用 Trae 同款的「默认档 → 更大档 + 开关」。开启时把该模型的最大窗口作为请求的 `context_length` 发出；关闭时清除覆盖值，请求回落到模型默认窗口。窗口数值来自 Qoder 自身目录（`default_context_window` / `available_context_windows`），不再是硬编码的 180000，因此 glm-5.3-flash 显示 1M 而不再被压到 180k。国际版与国内版均适用。
- 账号的模型列表现在会显示 Qoder 模型价格。Qoder 每个模型都带 `price_factor`（其客户端显示为 `0.50x Credit` 的那个倍率）以及 `is_free`/`tags`，但 worker 之前把它们丢掉了，所以控制台对所有 Qoder 账号都不显示价格。现在 worker 透传这些字段，适配层把倍率渲染为控制台的额度文案。免费标记对齐 Qoder 客户端自身的规则——带 `limited_time_free` 标签（或倍率为 0）才算免费，正倍率即视为计费——因此像 Qwen3.8-Max 这种「`is_free` 为真但同时带 0.5 倍率」的模型会显示倍率，而不再出现自相矛盾的「免费 / x0.5」。Qoder 国际版与国内版均适用。
- Qoder 账号现在显示真实剩余额度：账号卡片的主数值改为把「套餐额度 + 加量包 + 组织资源包」三桶相加，而不是只读套餐额度。每日签到奖励在上游是记入加量包的，因此此前仅靠套餐额度的账号在签到后卡片数字始终不动（例如一直停在 300，奖励金额不显示）。这与 Qoder 客户端自身的合计口径一致。

## 0.6.6 - 2026-09-24

### English

- Devin: resolve `chat_model_uid` from the live model catalog instead of hardcoded suffix tables, so renamed or removed thinking variants (e.g. `swe-1-7`, `glm-5-2`) no longer emit stale upstream model IDs. "None" is never chosen as an implicit default effort.
- Devin: pass images embedded in tool results through to the upstream prompt instead of dropping them.
- Stack Devin daily, weekly, and monthly quota meters as separate full-width rows on the account card, instead of sitting side by side.
- Expose WorkBuddy credit-pack expiry in `GET /api/accounts` quota: a new `packages` array carries each pack's remain/used/size plus its `CycleEndTime` (as `end_time` and a Unix `ends_at`), and the top-level `expires_at` / `expiring_remain` report the soonest expiry and how much remaining credit lapses then. The console quota tooltip now shows "N credits expire on D". All fields are `omitempty`; providers that do not report expiry (Trae, Qoder) simply omit them.

### 中文

- Devin：`chat_model_uid` 改为按实时模型目录解析，不再使用硬编码后缀表；上游已改名或移除的思考档变体（如 `swe-1-7`、`glm-5-2`）不会再发出过期模型 ID。默认档不会隐式选择 `none`。
- Devin：工具结果中携带的图片现在会透传给上游，不再被丢弃。
- Devin 日额度、周额度、月额度在账号卡片上各自单独占一行铺满，不再并排挤在一起。
- 在 `GET /api/accounts` 的 quota 中透出 WorkBuddy 积分包到期时间：新增 `packages` 数组按包返回 remain/used/size 以及 `CycleEndTime`（`end_time` 原始串与 Unix 秒 `ends_at`），顶层 `expires_at` / `expiring_remain` 给出最近一次到期时间及该时点将过期的剩余量；控制台配额 tooltip 现在会显示「N 积分将于某日到期」。所有字段均为 `omitempty`，不上报到期信息的 provider（Trae、Qoder）保持缺省。

## 0.6.5 - 2026-09-22

### English

- Console copy buttons now fall back to a hidden-textarea copy when the async Clipboard API is unavailable, so copying works when the console is served over plain http (a non-secure context), not just https/localhost.
- Trae account quota now counts only the General credit bucket; the Work-only bucket (parsed separately) is excluded so it is not summed into the General figure.
- Stretch Devin daily and weekly quota meters across the account card, with the title, used percent, bar, and reset time in one compact stack.
- Add concise acknowledgements for open-source projects that informed the project.
- Preserve cached input token usage in Responses API output for both streaming and non-streaming requests without double-counting total tokens.
- Map upstream `finish_reason: length` results to the Responses API `incomplete` terminal state, including streaming events, request logs, statistics, and UI filters.
- Recover Responses requests from malformed historical function-call arguments by skipping the invalid call/output pair, and avoid emitting invalid JSON arguments in generated Responses output.
- Trae max mode is now only offered on models that declare a real second tier. Models upstream tags for max mode without a larger window or larger ceilings no longer show a toggle that changes nothing (affects Doubao-Seed-2.1-Turbo, kimi-k2.6, kimi-k2.7-code).
- Turning Trae max mode off now restores the default-tier prompt/output ceilings immediately. The toggle only switches the context window server-side; the console derives the shown ceilings from the Max tier at render time, so the previously stuck Max values (e.g. 936k prompt / 64k output) no longer linger until a full catalog refresh.
- Trae login now uses the IDE (PKCE) authorization-code flow: the login URL carries a S256 `code_challenge`, and the pasted callback's `authCodeInfo` code is exchanged at `/trae/api/v3/oauth/ExchangeToken` with the matching verifier and a device public key (EC P-256). Accounts created before the switch keep refreshing with the OAuth client that minted their token (`refresh_client_id`).
- The Trae model catalog now also fetches a second scene (`chat_v3`) and merges it into the primary scene, so models the primary catalog hides as invisible become selectable. Duplicates prefer the entry carrying a credit rate.
- Trae chats are now routed to the catalog scene that actually serves each model. The whole catalog is served through `chat_v3` (which also carries the Max-mode tiers), and only models that scene does not list fall back to `solo_work_lite`. Previously every chat went through `solo_work_lite`, so the six models that scene does not carry (Doubao-Seed-Code, deepseek-v4.1-flash, glm-5.3-flash, glm-5.3-flashx, kimi-k2.8-preview, qwen3.8-flash) failed with a param error, and Max mode was sent to a scene without Max tiers. This is derived from catalog membership, so new models route correctly on the next refresh without changes.
- Preserve WorkBuddy cache-read and cache-write token usage when converting aggregated streaming responses into non-streaming results.
- Retry WorkBuddy daily check-ins when the upstream temporarily reports that a request is still being processed.

### 中文

- 控制台复制按钮在异步剪贴板 API 不可用时回退到隐藏文本框 + execCommand，因此在纯 http（非安全上下文）下打开控制台时复制也能生效，不再只支持 https/localhost。
- Trae 账号额度只统计「通用积分」桶；「Work 专属积分」桶（单独解析）不计入，避免混入通用额度。
- Devin 日额度和周额度条铺满账号卡片，标题、已用百分比、进度条和重置时间落在同一组紧凑块里。
- 在 README 增加简短的开源项目致谢。
- 在 Responses API 的流式与非流式输出中保留缓存输入 Token 用量，同时避免在总 Token 数中重复计数。
- 将上游 `finish_reason: length` 结果映射为 Responses API 的 `incomplete` 终止状态，并同步支持流式事件、请求日志、统计与界面筛选。
- 当 Responses 历史记录包含格式错误的函数调用参数时，跳过对应的调用与输出以恢复请求，并避免在生成的 Responses 输出中发送无效 JSON 参数。
- Trae 的「更大上下文」开关现在只在**确有第二档**的模型上出现。上游标了 max mode 但没有更大窗口、也没有更大上限的模型，不再显示一个点了没反应的开关（涉及 Doubao-Seed-2.1-Turbo、kimi-k2.6、kimi-k2.7-code）。
- 关掉 Trae max mode 后，输入/输出上限**立即**回到默认档。开关在服务端只切上下文窗口；控制台展示层在渲染时从 Max 档取值，因此之前会残留的最大值（如 936k 输入 / 64k 输出）不再需要整表刷新才恢复。
- Trae 登录改用 IDE（PKCE）授权码流程：登录链接带 S256 `code_challenge`，粘贴回调里的 `authCodeInfo` code 连同 verifier 与设备公钥（EC P-256）交到 `/trae/api/v3/oauth/ExchangeToken` 换取令牌。切换前创建的账号继续用签发其 refresh token 的 OAuth client 刷新（`refresh_client_id`）。
- Trae 模型目录额外拉取 `chat_v3` 场景并与主场景合并，使主目录里被标为 invisible 的模型变为可选；重复项优先保留带倍率的条目。
- Trae 聊天现在按「实际提供该模型的目录场景」路由。整个目录默认走 `chat_v3`（该场景也是唯一带 Max 档的），仅当 `chat_v3` 不收录某模型时才回退到 `solo_work_lite`。此前所有聊天一律走 `solo_work_lite`，导致该场景没有的 6 个模型（Doubao-Seed-Code、deepseek-v4.1-flash、glm-5.3-flash、glm-5.3-flashx、kimi-k2.8-preview、qwen3.8-flash）报参数错误，且 Max 模式被发到了没有 Max 档的场景。该路由由目录收录关系推导，新模型下次刷新即自动归位，无需改码。
- 将 WorkBuddy 聚合流式响应转换为非流式结果时，保留缓存读取与缓存写入 Token 用量。
- 当 WorkBuddy 上游暂时返回“请求处理中”时，自动重试每日签到。

## 0.6.4 - 2026-09-19

### English

- Send Qoder CN campaign check-in with the official desktop `Cosy-ClientType` headers so the daily credit activity is not filtered out as unavailable.

### 中文

- Qoder 国内版活动签到补上官方桌面端 `Cosy-ClientType` 请求头，避免每日积分活动被过滤成未开放。

## 0.6.3 - 2026-09-19

### English

- Show Devin daily and weekly included usage as separate account-card meters, including reset time, instead of collapsing them into one tighter percentage.
- Trae CN check-in now sends the device id in the `aha-<hex>` shape the check-in backend expects, so accounts added through the login flow claim their daily credits instead of always failing with `9074 当前参与用户太多`.

### 中文

- Devin 账号卡片按日额度和周额度分开展示，并带重置时间，不再把两条额度压成一条更紧的百分比。
- Trae CN 签到改为按后端要求的 `aha-<hex>` 格式发送设备 ID，通过登录流程添加的账号现在能正常领取每日积分，不再一直报 `9074 当前参与用户太多`。

## 0.6.2 - 2026-09-19

### English

- Collect upcoming release notes as per-PR files in `changelog/unreleased/` instead of a shared Unreleased section that had to be frozen after each tag.
- Switch Qoder CN check-in to the official campaign claim API (`/sash/api/v1/me/campaigns/{id}/claim`) instead of the retired daily-check-in endpoints.

### 中文

- 即将发布的说明改到 `changelog/unreleased/` 按 PR 分文件记录，不再共用 Unreleased 小节、发完再冻结。
- Qoder 国内版签到改为官方活动领取接口（`/sash/api/v1/me/campaigns/{id}/claim`），不再使用已下线的 daily-check-in。

## 0.6.1 - 2026-09-19

### English

- Unify WorkBuddy and Qoder CN check-ins with provider default times, inheritable account overrides, and one scheduler. Keep check-in controls and history on account cards, without a separate navigation entry. Preserve existing WorkBuddy settings; Qoder CN live acceptance remains pending.
- Move the WorkBuddy drop-system-prompt switch off account cards; keep the control and explanation in create and edit.
- Record requested and resolved reasoning levels on request history; show one value when they match, otherwise `requested → resolved`.
- Align the README header mark with the CLI2API wordmark so the C icon centers on the text cap height instead of dropping below the baseline.

### 中文

- 统一 WorkBuddy 与 Qoder 国内版签到：供应商默认时间、账号动态继承/覆盖、公共调度；签到操作和记录留在账号页，不另设签到中心。保留已有 WorkBuddy 设置，Qoder 国内版真实账号验收仍待完成。
- 从账号卡片上移除 WorkBuddy「丢弃系统提示词」开关，改到新建和编辑里，并保留说明文案。
- 请求历史记录请求传入与实际上游的推理强度；两者一致时只显示一档，不一致时用 `请求 → 实际`。
- 让 README 头部的 C 标记与 CLI2API 标题对齐，图标与文字大写高度居中，不再明显低于基线。

## 0.5.7 - 2026-09-18

### English

- Preserve typed upstream stream errors through the OpenAI, Anthropic, and Responses relays so invalid Devin requests do not falsely cool accounts, while transport interruptions remain retryable
- Report Devin cache reads and writes in OpenAI-compatible usage, with prompt
  totals including all upstream input tokens
- Generate Devin chat and account-status protobuf types from extracted descriptors
  with a manual update command; builds and CI use committed Go bindings without
  downloading releases or regenerating schemas
- Neutralize Codex/Desktop MCP-looking tool names for Devin (`mcp__*`, `list_mcp_*`, and any name containing `mcp`) into reversible `cx_tool_*` aliases, restore the originals on tool calls for local execution, scrub residual MCP text on fallback, and if upstream still returns an MCP configuration `permission_denied` retry by stripping those tools then keeping only core local tools (`exec_command` / `write_stdin` / `view_image` / `request_user_input`) with minimal schemas; log each fallback stage and never drop all tools
- Ignore orphan `tool_choice` when Codex compact / recovery turns send a choice with no remaining tools, instead of failing with `tool_choice requires tools`
- Keep session affinity from pinning a later model onto an empty-catalog account, so a Deepseek compact after a Devin turn routes to WorkBuddy instead of Devin

### 中文

- OpenAI、Anthropic 与 Responses 流式转发会保留上游的类型化错误，避免无效的 Devin 请求被错误地冷却账号，同时传输中断仍可重试
- Devin 的缓存读取与写入会显示在 OpenAI 兼容 usage 中，prompt 总数包含全部上游输入 token
- 新增手动更新命令，提取 descriptor 并生成 Devin 聊天与账号状态 protobuf 类型；构建与 CI 直接使用已提交的 Go 文件，不下载发行包或重新生成 schema
- Devin 会把 Codex/Desktop 带 MCP 语义的工具名（`mcp__*`、`list_mcp_*` 以及名称含 `mcp` 的工具）中性化为可逆的 `cx_tool_*` 别名，并在返回的 tool_calls 中还原原名供本地执行；若上游仍返回 MCP 配置类 `permission_denied`，会先清洗残留 MCP 文案并去掉这些工具再试，再失败则只保留核心本地工具（`exec_command` / `write_stdin` / `view_image` / `request_user_input`，最小 schema），每次 fallback 都会打日志，且不再清空全部 tools
- Codex compact / 恢复轮次如果带了 `tool_choice` 但 tools 已被规范化为空，会忽略这个孤立的 `tool_choice`，不再报 `tool_choice requires tools`
- 会话粘性不会再把后续模型钉到空 catalog 账号上，因此 Devin 之后的 Deepseek compact 会走 WorkBuddy，而不是误打到 Devin

## 0.6.0 - 2026-09-19

### English

- Preserve cancellation and deadline causes in stream read failures without cooling healthy accounts; classify typed upstream stream errors once while retaining the original error chain.
- Keep Trae and WorkBuddy model settings marked as default after refresh when the selected reasoning level matches the catalog default; ignore inactive Max settings in the custom-state indicator.
- Clarify setup, administrator versus client keys, compatibility limits, and managed updates; align documentation and README artwork with the accepted refactor boundaries without claiming pending acceptance is complete.
- Keep console-key rotation synchronized with live HTTP authentication and Qoder worker requests without restarting the API server
- Remove per-request update dependency rewrites and restore thread-safe Qoder starter configuration during proxy/key changes
- Restore Responses function namespaces in JSON and SSE output, and preserve qualified tool identities when replaying calls or selecting a function.
- Preserve Qoder user images and image-bearing tool results, emitting tool-result images after their complete ordered tool batch.
- Bridge Responses custom tools through function calls, restoring custom output/events and replaying tool results. Format rules are descriptive, not grammar-enforced; custom input events are emitted after argument collection.

### 中文

- 流读取失败保留取消与超时原因，不再误冷却健康账号；上游类型化流错误统一分类一次，同时保留原始错误链。
- Trae、WorkBuddy 选择目录默认推理等级后，刷新仍显示默认状态；未生效的 Max 设置不再误标为自定义。
- 精简安装与接入说明，区分管理员和客户端密钥，明确兼容范围与托管更新流程；按已验收重构边界同步文档和 README 配图，不将待验收事项写成已完成。
- 控制台密钥轮换会同步到正在使用的 HTTP 鉴权和 Qoder worker 请求，无需重启 API 服务
- 移除更新接口逐请求重写依赖的竞态，并恢复代理/密钥变更时 Qoder 启动器配置的并发安全
- Responses 的 JSON 和 SSE 输出会还原 function 的命名空间，历史调用回放与指定函数选择也会保留完整工具身份。
- 保留 Qoder 用户消息及工具结果中的图片，并在完整、有序的工具结果批次之后发送工具图片。
- 通过 function 调用桥接 Responses custom 工具，还原 custom 输出与事件并回放工具结果。格式规则仅作为描述传递，不强制执行语法约束；custom 输入事件在参数收集后发送。

## 0.5.6 - 2026-09-17

### English

- Expand Codex/Desktop `type: "namespace"` tool wrappers into plain function tools (nested names qualified as `namespace__name`) for the Responses adapter, Trae, and WorkBuddy, and drop hosted shells such as `type: "mcp"` / `web_search` that upstreams reject
- Replay Responses `reasoning` items onto the following assistant message or function call as `reasoning_content`, so WorkBuddy thinking-mode history survives translation
- Accept content-block arrays in Responses `function_call_output` and lift `input_image` blocks into a following user message

### 中文

- Responses 适配层、Trae 与 WorkBuddy 都会把 Codex/Desktop 的 `type: "namespace"` 工具包装展开成普通 function（嵌套名限定为 `namespace__name`），并丢弃上游会拒绝的 `type: "mcp"` / `web_search` 等 hosted 外壳
- Responses 的 `reasoning` 条目会作为 `reasoning_content` 回放到紧随其后的 assistant 消息或函数调用上，WorkBuddy 思考模式的历史得以保留
- Responses 的 `function_call_output` 支持内容块数组，并把 `input_image` 块提升为随后的 user 消息

## 0.5.5 - 2026-09-16

### English

- Source WorkBuddy model credits and free badges from the official catalog instead of hardcoded prices, and let the Providers page request `/api/models?view=regional` so each provider region keeps its own price while Access/Overview stay on the merged catalog
- When the same model is merged across regions with conflicting credits, omit the price on the merged `/v1` and default `/api/models` rows instead of keeping the first account's rate
- Show consumed points under the Tokens column in request history when a provider reports them; keep writing the value on the request log row, and fall back to the request-detail table for rows that only stored it there
- Temporary Devin diagnostic: when upstream returns an MCP configuration `permission_denied`, append a compact tools type/name summary (`tools_diag`) to the error so request logs can show what Desktop sent versus what was forwarded

### 中文

- WorkBuddy 模型积分与免费标记改为读取官方目录；Providers 页通过 `/api/models?view=regional` 按供应商区域展示各自价格，Access/Overview 仍使用合并目录
- 同一模型跨区域合并且价格冲突时，合并后的 `/v1` 与默认 `/api/models` 条目会省略价格，不再保留首个账户的费率
- 请求历史上游若回报消耗点数，会在 Tokens 列下方以绿色小字展示；继续写入请求日志主表对应字段，并对仅记在详情表的历史行做回退读取
- 临时诊断：Devin 上游返回 MCP 配置类 `permission_denied` 时，会在错误信息追加精简的 tools type/name 摘要（`tools_diag`），便于从请求日志对照 Desktop 入站与实际上游转发内容

## 0.5.4 - 2026-09-16

### English

- Alias Codex/Desktop `mcp__*` tools for Devin upstream and restore the original names on tool calls so the local client can execute MCP, while still treating MCP configuration `permission_denied` as an invalid request instead of auth cooldown
- Expand Codex/Desktop `type: "namespace"` tool wrappers into plain function tools for Devin and drop hosted shells such as `type: "mcp"` / `web_search`, which were enough to trip the upstream provider
- Record each request's consumed points (WorkBuddy `usage.credit`) in a dedicated request-detail table and show them in the console request detail, keeping the query-oriented request log row lean

### 中文

- Devin 会对 Codex/Desktop 的 `mcp__*` 工具做上游别名并在返回的 tool_calls 中还原原名，便于本地客户端执行 MCP；MCP 配置类 `permission_denied` 仍归为无效请求，不再按鉴权失败冷却账号
- Devin 会把 Codex/Desktop 的 `type: "namespace"` 工具包装展开成普通 function，并丢弃 `type: "mcp"` / `web_search` 这类 hosted 外壳；此前仅这些外壳就足以让上游失败
- 每次请求消耗的点数（WorkBuddy `usage.credit`）记入独立的请求详情表，并在控制台请求详情中展示；面向查询的请求日志主表保持精简

## 0.5.3 - 2026-09-15

### English

- Add experimental Devin support (`provider=devin`) with browser OAuth or session-token import, Connect-RPC chat stream/non-stream, and a fail-closed TTL model catalog; not claimed production-ready

### 中文

- 新增实验性 Devin 支持（`provider=devin`）：可用浏览器 OAuth 或导入 session token，支持 Connect-RPC 聊天流式/非流式，以及失败即显式报错的 TTL 模型目录；尚未宣称生产可用

## 0.5.2 - 2026-09-15

### English

- Stop rewriting WorkBuddy `deepseek-v4.1-flash` to the stale `deep-model` upstream ID when the live catalog already exposes the native spelling, so chat no longer silently falls back to other models

### 中文

- WorkBuddy 在线上目录已原生提供 `deepseek-v4.1-flash` 时，不再把它改写成过期的上游 ID `deep-model`，避免聊天静默落到其他模型

## 0.5.1 - 2026-09-15

### English

- Allow named API keys to restrict routing to specific provider regions while keeping legacy family grants compatible, and show the same region-aware scope in the model catalog

### 中文

- 支持将命名 API Key 的路由限制到具体供应商区域，同时兼容旧的供应商族授权，并让模型目录使用相同的区域权限过滤

## 0.5.0 - 2026-09-15

### English

- Accept `tool_reference` blocks inside Anthropic tool results and keep them as text instead of rejecting the whole request
- Route outbound traffic through a global HTTP(S) proxy, with an optional per-account override (`direct` / `none` for explicit direct connections; WorkBuddy and Trae account overrides also accept SOCKS5)
- Expose WorkBuddy `deepseek-v4.1-flash` through the catalog alias so the model routes instead of being rejected
- Normalize display-name model inputs (for example `DeepSeek: DeepSeek V4.1 Flash`) to canonical IDs before routing, and classify client-canceled requests as canceled instead of unavailable
- Align create-account and edit-account form fields, and let WorkBuddy accounts set their own daily check-in time
- Add a system-wide WorkBuddy check-in default that new accounts inherit

### 中文

- 接受 Anthropic 工具结果中的 `tool_reference` 块并保留为文本，不再整条请求报错
- 支持统一 HTTP(S) 出站代理，并可对单个账号设置覆盖（`direct` / `none` 显式直连；WorkBuddy 与 Trae 的账号级代理还支持 SOCKS5）
- WorkBuddy 通过目录别名暴露 `deepseek-v4.1-flash`，模型可以正常路由而不再被拒绝
- 将 display-name 形式的模型输入（如 `DeepSeek: DeepSeek V4.1 Flash`）归一化为规范 ID 后再路由，并把客户端主动取消的请求归类为“已取消”而非“不可用”
- 对齐创建和编辑账号表单，并允许 WorkBuddy 账号单独设置每日签到时间
- 系统设置增加 WorkBuddy 默认签到时间，新建账号会继承该时间

## 0.4.10 - 2026-09-12

### English

- Allow CORS preflight requests on the OpenAI-compatible endpoints without weakening API-key authentication on actual requests
- Repair WorkBuddy tool history after a canceled tool round so the next turn is not rejected as a broken tool sequence
- Send WorkBuddy Deepseek V4.1 Flash thinking as official top-level reasoning fields so streamed thinking comes back
- Show WorkBuddy catalog context budgets (default and optional window) on the model list without inventing a Trae Max switch

### 中文

- OpenAI 兼容接口允许跨域预检请求，但实际请求仍必须通过 API Key 认证
- WorkBuddy 在工具调用中途停止后，下一轮会修好不完整的 tool 记录，避免被当成工具序列损坏拒绝
- WorkBuddy 的 Deepseek V4.1 Flash 改为发送官方顶层思考字段，流式思考内容可以返回
- 模型列表展示 WorkBuddy 目录里的默认和可选上下文窗口，不套用 Trae 的更大上下文开关

## 0.4.8 - 2026-09-11

### English

- Preserve cached model routes when the dynamic model catalog is temporarily unavailable, while refreshing genuine model misses
- Let WorkBuddy accounts choose their automatic daily check-in time during creation, with a 21:00 retry for failed attempts
- Enlarge the account-name input in the create-account wizard
- Open request-history details immediately with a loading skeleton while the full record is fetched

### 中文

- 动态模型目录暂时不可用时保留已有模型路由，真实模型缺失时立即刷新目录
- WorkBuddy 账号创建时可选择每日自动签到时间，失败会在 21:00 重试
- 放大创建账号向导中的账号名称输入框
- 点击请求历史后立即打开详情弹窗，并在完整记录获取期间显示骨架屏

## 0.4.7 - 2026-09-10

### English

- Restore opening request-history details by using the table row action event supported by HeroUI

### 中文

- 改用 HeroUI 支持的表格行操作事件，恢复点击请求历史查看详情

## 0.4.6 - 2026-09-10

### English

- Show missing Qoder account quota as loading or unavailable instead of incorrectly labeling it as requiring login

### 中文

- 将 Qoder 账号缺失的额度信息显示为“额度获取中”或“额度不可用”，避免误显示为需要登录

## 0.4.5 - 2026-09-10

### English

- Open runtime log lines in a larger scrollable HeroUI detail modal, and align request-log details with the same roomy layout

### 中文

- 运行日志行可打开更大的可滚动 HeroUI 详情弹窗，并同步优化请求日志详情的宽度与布局

## 0.4.4 - 2026-09-10

### English

- Keep Qoder accounts routable when their base quota is exhausted but an available add-on or organization resource package still has credits, and show resource-package balance in the console

### 中文

- Qoder 主额度用尽但可用附加包或组织资源包仍有余额时继续参与路由，并在控制台展示资源包余额

## 0.4.3 - 2026-09-10

### English

- Remove the fixed 120-second timeout from WorkBuddy non-streaming chat aggregation and preserve upstream read errors instead of reporting a missing [DONE] marker

### 中文

- 移除 WorkBuddy 非流式聊天聚合的固定 120 秒超时，并保留上游读取错误，避免误报缺少 [DONE] 标记

## 0.4.2 - 2026-09-09

### English

- Return cached provider models immediately while refreshing expired catalogs in the background, and deduplicate concurrent catalog loads

### 中文

- 供应商模型目录优先立即返回缓存，并在缓存过期后后台刷新，同时合并并发目录请求

## 0.4.1 - 2026-09-09

### English

- Normalize empty WorkBuddy message content, preserve tool-call messages, and record non-sensitive message-shape diagnostics for request troubleshooting

### 中文

- 规范化 WorkBuddy 的空消息内容，保留工具调用消息，并记录不含正文的消息结构诊断，方便排查请求问题

## 0.4.0 - 2026-09-09

### English

- Load the overview dashboard from a lightweight summary first, fetch account and model details per page in parallel, and cache request statistics briefly to reduce SQLite work during console startup

### 中文

- 首页先加载轻量概览摘要，账号和模型详情按页面并行请求，并短暂缓存请求统计，减少控制台启动时对 SQLite 的读取压力

## 0.3.7 - 2026-09-09

### English

- Add asynchronous stream diagnostics for cancellation source, upstream status, SSE progress, and incomplete streams
- Classify client and request-context stream cancellations separately from upstream unavailability
- Show stream diagnostics in request details without storing prompt or response content

### 中文

- 异步记录流取消来源、上游状态、SSE 进度和未完成流等诊断信息
- 将客户端断开和请求上下文取消与上游不可用分开归类
- 请求详情展示流诊断，但不保存提示词或响应正文

## 0.3.6 - 2026-09-08

### English

- Avoid refreshing every account during console initialization, preventing credential payload timeouts when the account list is large

### 中文

- 控制台初始化不再刷新全部账号，账号较多时不会因排队读取 credential payload 而超时

## 0.3.5 - 2026-09-08

### English

- Persist account quota snapshots and quota-exhausted state in SQLite so account filters and pagination use stored state without refreshing every account on page load
- Add compact provider filters with account counts and make the Accounts page show 20 accounts per page by default
- Simplify the Access page layout and tighten supported endpoint cards
- Accept completed Qoder SSE streams that omit a final [DONE] marker when a finish reason was already received

### 中文

- 将账号额度快照和「额度已用尽」状态持久化到 SQLite，账号筛选和分页直接使用已存状态，不再在页面加载时逐个刷新
- 账号页新增带数量的供应商筛选，并将默认每页显示改为 20 个账号
- 简化 Access 页面布局，收紧支持端点卡片
- Qoder SSE 已收到结束原因但缺少最终 [DONE] 标记时，仍按正常完成处理

## 0.3.4 - 2026-09-07

### English

- Strip empty WorkBuddy chat-stream deltas so clients do not render a flood of blank thinking chunks

### 中文

- WorkBuddy 的 chat 流式不再带上空的 `content` / `reasoning_content`，避免客户端刷出空白思考

## 0.3.3 - 2026-09-07

### English

- Drop the System page version-history hint; the list already opens one release at a time

### 中文

- 系统页历史版本不再显示「一次只展开一条」的说明，列表本身就是一次只开一个版本

## 0.3.2 - 2026-09-07

### English

- Show a compact version history on the System page: one release open at a time, with notes and restore for the three previous versions
- Use plain update copy on the System page: download the update, tap Update now, then refresh after a short countdown
- Explain Models-page defaults: Trae Max is a larger context window, and reasoning intensity is a fallback when the request omits it
- Skip WorkBuddy check-in for the rest of the local day after a success or already-checked-in result, and treat HTTP 400 “already checked in” as that result instead of a failure

### 中文

- 系统页用折叠列表展示近期版本：一次只展开一条，可看说明，并可恢复到最近三个旧版本
- 系统页更新文案改为普通说法：先下载更新，再点立即更新，完成后倒计时刷新页面
- 模型页补充说明：Trae 的 Max 是更大上下文，推理强度只是请求未指定时的默认档，不会锁死每次调用
- 当天本地日历日已签到成功或确认「已签到」后，不再重复打上游、不再写入签到记录；HTTP 400 的「今天已签到」按已签到处理，不再记成失败

## 0.3.1 - 2026-09-07

### English

- Share one provider, region, and model match across API-key allowlists, sticky sessions, and pool picks, and reuse the same chat preflight for `/v1` and compatibility routes
- Canonicalize mixed-case provider IDs when storing accounts and looking up in-process adapters so a routed WorkBuddy account cannot miss its adapter
- Document provider-prefixed model IDs on the Access page, and keep named API keys limited to those providers for both chat routing and `/v1/models`

### 中文

- API key 白名单、会话粘滞和池调度共用同一套供应商 / 区域 / 模型匹配；`/v1` 与兼容接口共用同一套请求预检
- 账号入库和 in-process adapter 查找都按规范供应商 ID，避免 `WorkBuddy` 这类大小写混写选中后找不到执行器
- API 接入页说明可用模型前缀指定供应商；客户端密钥在对话调度和 `/v1/models` 上都只看到允许的供应商

## 0.3.0 - 2026-09-06

### English

- Keep multi-turn conversations on the same account from the first user message by default, including image-only turns, without requiring `X-CLI2API-Session`
- Cache `GET /api/models` for 5 minutes so the console catalog page does not re-hit WorkBuddy or Trae on every load; `?refresh=1` still fetches live. Overview stays uncached.

### 中文

- 同一段多轮对话默认按首条用户消息（含纯图片）粘到同一个账号，不再需要 `X-CLI2API-Session`
- `GET /api/models` 缓存 5 分钟，控制台模型页不再每次都打 WorkBuddy / Trae 目录；`?refresh=1` 仍即时拉取。Overview 不缓存。

## 0.2.48 - 2026-09-06

### English

- Replace the running host-updater binary in place after a managed update and exit so systemd or LaunchAgent starts the new process
- Roll back a failed host-binary swap with the container, stamp updater version into release assets, let an old updater complete one jump, and offer the three previous stable releases on the System page

### 中文

- 托管更新成功后就地替换正在运行的宿主机更新器二进制并退出，由 systemd / LaunchAgent 拉起新进程
- 宿主机二进制替换失败时连容器一起回滚；Release 附件打上 updater 版本号；旧更新器可完成一次升级；系统页可回滚到最近三个稳定版

## 0.2.47 - 2026-09-06

### English

- Show staged update progress and host-updater errors on the System page, instead of leaving the button stuck on Updating
- Keep the update card visible while checking GitHub, reload after a successful restart, and allow cancelling a hung image download or discarding a prepared image

### 中文

- 系统页展示分阶段更新的真实进度和宿主机更新器错误，避免按钮一直停在「更新中」
- 检查更新时不再拆掉更新卡片；重启成功后自动刷新；下载卡住可取消，已下载镜像可放弃

## 0.2.46 - 2026-09-06

### English

- Recommend Docker Compose as the supported install and managed-update path in the README and deployment guide
- Keep an account on a model route after a later catalog refresh omits an ID it already served, and report pool-wide quota cooldown as `insufficient_quota` instead of a generic rate limit

### 中文

- 在 README 与部署说明中明确推荐 Docker Compose 作为官方安装与托管更新路径
- 账号已成功服务过的模型，在后续目录刷新漏掉该 ID 时仍可继续路由；全池额度冷却改为返回 `insufficient_quota`，而不是笼统的限流错误

## 0.2.45 - 2026-09-05

### English

- Add console-selectable account routing strategies: round-robin, weighted round-robin, and fill-first
- Recover failed account runtimes with exponential backoff, and surface starting, recovering, and auth-failed states on account cards
- Show session-affinity TTL, hits, misses, escapes, and the last miss/escape reason on the System page
- Configure per-account concurrency in the console instead of the global `QODER_MAX_INFLIGHT` environment variable
- Align the README and deployment documentation with current provider capabilities, session affinity, API routes, and per-account concurrency settings

### 中文

- 控制台可选择账号调度策略：轮询、加权轮询、填满优先
- 账号运行时启动失败后按指数退避自动恢复，并在账号卡片上展示启动中、恢复中、登录失败状态
- 在系统页展示会话粘性 TTL、命中、未命中、逃逸，以及最近一次未命中/逃逸原因
- 账号并发改为控制台按账号配置，不再使用全局环境变量 `QODER_MAX_INFLIGHT`
- 同步 README 与部署文档，修正当前 Provider 能力、会话粘性、API 端点和账号级并发配置说明

## 0.2.44 - 2026-09-04

### English

- Document the staged update flow and its restart confirmation boundary for the next patch release

### 中文

- 补充分阶段更新流程及重启确认边界的发布说明

## 0.2.43 - 2026-09-04

### English

- Polish the API access console with connection status, compact endpoint cards, and copy/open actions for supported routes
- Expose the Anthropic Messages and OpenAI Responses routes in the console access catalog and overview metadata
- Split managed updates into image preparation and operator-confirmed restart, with durable progress states and rollback kept on the restart step

### 中文

- 优化 API 接入控制台，增加连接状态、紧凑端点卡片，以及支持端点的复制和打开操作
- 在控制台接入目录和概览元数据中展示 Anthropic Messages 与 OpenAI Responses 端点
- 托管更新拆为镜像准备和人工确认重启两步，过程状态可追踪，重启步骤仍保留回滚

## 0.2.42 - 2026-09-04

### English

- Distinguish hard quota exhaustion from prompt token limits, and cool exhausted accounts until the next local midnight
- Keep explicitly identified conversations on one account with bounded in-memory session affinity, same-provider/region escape, and routing-source request logs
- Add stateless Anthropic Messages and OpenAI Responses adapters for text and function-tool conversations

### 中文

- 区分账号额度耗尽和请求 token 超限，真正额度耗尽的账号冷却到本地次日凌晨
- 增加基于显式会话标识的有界内存会话粘性路由、同提供方同区域逃逸，以及路由来源请求日志
- 新增面向文本与函数工具对话的无状态 Anthropic Messages 与 OpenAI Responses 适配层

## 0.2.41 - 2026-09-03

### English

- Make the account toolbar compact enough to keep common actions visible, with overflow reserved for secondary actions
- Reduce account card density so account lists are easier to scan

### 中文

- 收紧账号工具栏，让常用操作直接展示，只把次要操作放进更多菜单
- 降低账号卡片的信息密度，让账号列表更容易浏览

## 0.2.40 - 2026-09-03

### English

- Interrupt active connections instead of waiting for in-flight requests before managed updates; clients can retry interrupted requests
- Load the Accounts page first, refresh account details in small batches, and default pagination to five accounts
- Add compact WorkBuddy check-in status, check-in history, manual check-in, and scheduled daily check-ins around 09:00 and 21:00 local time

### 中文

- 托管更新前不再等待在途请求结束，而是直接中断现有连接并由客户端重试
- Accounts 页面先加载账号列表，再按小批次刷新账号详情，默认分页调整为每页 5 个账号
- 增加紧凑的 WorkBuddy 签到状态、签到记录、手动签到，以及本地时间约 09:00 和 21:00 的每日自动签到

## 0.2.39 - 2026-09-03

### English

- Prevent system updates and overview loading from reusing expired account-refresh contexts while checking account state

### 中文

- 修复系统更新和 Overview 加载复用已过期账号刷新 context 的问题，避免账号列表查询超时

## 0.2.38 - 2026-09-03

### English

- Keep cached account cards visible while health, quota, and model details refresh asynchronously, with inline skeletons and usable pagination

### 中文

- 账号健康状态、额度和模型详情异步刷新期间保留缓存卡片，并显示局部骨架屏，分页仍可用

## 0.2.37 - 2026-09-03

### English

- Make console updates asynchronous: return immediately with a local job ID and show release checks, request draining, SQLite backup, submission, updater progress, failures, and automatic reload as one continuous status flow
- Restore update progress after a page refresh by exposing the preparation job from the system update endpoint

### 中文

- 控制台更新改为异步执行：立即返回本地任务 ID，并连续展示版本检查、等待请求结束、SQLite 备份、提交、宿主机更新、失败和自动刷新状态
- 系统更新接口返回准备任务状态，页面刷新后可以恢复显示更新进度

## 0.2.36 - 2026-09-03

### English

- Add a configurable per-request retry-account budget, with `QODER_MAX_RETRY_ACCOUNTS` capped at 64
- Parse `Retry-After` and reset hints from duration, date, Unix timestamp, and nested provider error payloads; protect short rate-limit hints with a 30-second floor
- Normalize streaming failures into structured OpenAI-compatible SSE error frames and report incomplete or interrupted upstream streams explicitly
- Improve account card text contrast in dark mode, including cooldown details, IDs, and usage metadata

### 中文

- 增加单次请求的可配置账号重试预算，支持 `QODER_MAX_RETRY_ACCOUNTS`，上限为 64
- 支持从 duration、日期、Unix 时间戳和嵌套 provider 错误中解析 `Retry-After`/重置提示，并为过短的限流提示设置 30 秒保护下限
- 将流式失败统一转换为 OpenAI 兼容的结构化 SSE 错误帧，并明确报告上游流中断或未完成
- 提升深色模式下账号卡片的文字对比度，优化冷却详情、账号标识和使用信息的可读性

## 0.2.35 - 2026-09-03

### English

- Exclude accounts marked not ready from routing so stale workers do not consume failover attempts

### 中文

- 路由时排除明确未就绪的账号，避免失效 Worker 消耗 failover 尝试

## 0.2.34 - 2026-09-02

### English

- Make account failure handling scope-aware: quota, authentication, readiness, and upstream failures cool the whole account, while rate limits remain isolated to the affected model; cooling accounts no longer receive requests
- Recognize nested CodeBuddy quota errors such as code `14018`, apply an account cooldown, and expose model-level cooldowns with precise `Retry-After` messages
- Improve the Accounts console with compact cards, a single primary refresh action, matching skeleton dimensions, and live per-model cooldown indicators

### 中文

- 完善账号错误范围处理：额度、认证、就绪状态和上游故障冷却整个账号；限流仍只隔离受影响的模型；冷却中的账号不再接收请求
- 支持识别 CodeBuddy 的嵌套额度错误（包括 `14018`），触发账号冷却，并通过明确的 `Retry-After` 提示模型级冷却
- 优化账号控制台：卡片更紧凑，只保留一个主要刷新操作，骨架屏尺寸与实际卡片匹配，并实时展示模型级冷却状态

## 0.2.33 - 2026-09-02

### English

- Load the Accounts page from a cached lightweight list first, then refresh account health and credits asynchronously; remove the duplicate worker rewarm button so each card keeps one clear refresh action
- Refresh a single account from its card: the button re-probes that one account's health, credits, and model catalog and shows a skeleton while it runs, instead of reloading the whole page

### 中文

- 账号页先加载轻量缓存列表，再异步刷新账号状态和额度；移除重复的 Worker 重启按钮，让每张卡片只保留一个明确的刷新操作
- 账号卡片支持单个刷新：按钮只重新探测该账号的运行状态、额度和模型目录，期间显示骨架屏，不再整页刷新

## 0.2.31 - 2026-09-01

### English

- Paginate the Accounts page so large pools render one page of cards at a time, with the same per-page control used on Logs and Models

### 中文

- 账号页支持分页：账号较多时按页展示卡片，每页数量选择与日志、模型页一致

## 0.2.30 - 2026-08-31

### English

- Clarify account quota usage on the Accounts page by showing used and remaining credits together

### 中文

- 账号页面同时显示已用和剩余额度，额度使用情况更清晰

## 0.2.29 - 2026-08-31

### English

- Fix uneven load across accounts: rotation used a numeric index into a candidate list that shrinks whenever a retry excludes an account or one enters cooldown, which silently re-seated the rotation and left some accounts nearly idle while others absorbed most traffic. Rotation now resumes from the previously picked account's ID, so it stays even as the candidate set changes
- Enforce the per-account concurrency limit: `max_inflight` was stored and shown in the console but never read, so a single account could absorb every concurrent request during a burst. Saturated accounts are now skipped in favour of ones with spare capacity
- Persist cooldowns to SQLite and restore them on start: cooldowns lived only in memory, so every managed update (they run daily) wiped them and let a just-quarantined account walk straight back into rotation
- Make the account priority field actually schedule traffic: it is now a weight (1–100) driving smooth weighted round-robin. Accounts at the default 50 keep plain round-robin, so existing setups behave exactly as before
- Cool down only the affected model: a rate limit or quota error on one model no longer takes the whole account offline for every other model
- Back off on repeated failures: an account failing repeatedly with the same error now cools down progressively longer instead of retrying at a fixed interval, up to 6 hours, and resets as soon as it succeeds
- Keep a route on one region: when no region is pinned, scheduling reuses the region it last served from instead of letting rotation decide, so a mixed-region pool no longer flips between regions

### 中文

- 修复账号间负载不均的问题：轮转此前用数值索引指向一个会收缩的候选列表（重试排除、账号冷却都会让它变小），这会静默重定位轮转位置，导致部分账号几乎空闲而另一些承担大部分流量。现在轮转从上次选中账号的 ID 继续，候选集变化时分布依然均匀
- 账号并发上限真正生效：`max_inflight` 此前只存储并在控制台展示，从未参与选号，突发流量下单个账号会吞掉全部并发请求。现在达到上限的账号会被跳过，优先调度有余量的账号
- 冷却持久化到 SQLite 并在启动时恢复：冷却此前仅存于内存，每次自动更新（每天执行）都会清空，刚被隔离的账号会立刻回到轮转中
- 账号优先级字段真正参与调度：现在作为权重（1–100）驱动平滑加权轮转。保持默认 50 的账号行为与之前完全一致
- 只冷却受影响的模型：单个模型限流或额度耗尽不再让整个账号对其他模型下线
- 重复失败时逐步退避：同一账号反复出现同类错误时，冷却时间会逐步延长（上限 6 小时），成功后立即归零
- 路由保持在同一个 region：未固定 region 时沿用上次服务的 region，而不是由轮转位置决定，混合 region 的账号池不再来回切换

## 0.2.28 - 2026-08-31

### English

- Fix Trae quota errors (code 4008) never failing over: Solo answers HTTP 200 and reports the quota failure later inside the SSE body, so the executor treated the attempt as successful and returned the error to the client. The rewritten stream now surfaces that terminal error once drained, and the API relays it back into the pool so the account is cooled for 6 hours instead of being retried while exhausted
- Fix WorkBuddy usage-limit cooldown ignoring the reset time the upstream reports: a `6004` message carries an absolute reset timestamp (`将在 2026-09-01 13:56:47 UTC+8 重置`), but the account only cooled for the generic 60-second fallback and immediately burned more quota. The cooldown now waits until that timestamp (interpreted as UTC+8) and falls back to 60 seconds when no reset time is present

### 中文

- 修复 Trae 额度错误（code 4008）从不故障切换的问题：Solo 先返回 HTTP 200，额度失败在 SSE 流体内稍后才到达，执行器因此把这次尝试当成成功并把错误直接抛给客户端。现在重写后的流会在读取完毕后抛出该终止错误，API 层再把它回填进调度池，使账号冷却 6 小时，而不是在额度耗尽期间被反复选中
- 修复 WorkBuddy 用量限流冷却忽略上游重置时间的问题：`6004` 报文中带有绝对重置时刻（`将在 2026-09-01 13:56:47 UTC+8 重置`），但账号此前只按通用 60 秒回退冷却，随即继续消耗额度。现在冷却会等到该时刻（按 UTC+8 解析），报文没有重置时间时才回退到 60 秒

## 0.2.27 - 2026-08-31

### English

- Fix Trae and WorkBuddy per-model settings (max mode, reasoning effort) never taking effect: the console stores them under the canonical model key while chat requests looked them up with a plain lowercase match, so mixed-case Trae model IDs and underscored public IDs missed the saved row; lookups now share one canonical key, cold-catalog refreshes no longer drop max mode, and a requested-but-unsupported max mode is logged instead of silently dropped

### 中文

- 修复 Trae / WorkBuddy 模型设置（max 模式、推理档位）从不生效的问题：控制台按规范化模型 key 存储，而聊天请求只用小写匹配查找，混合大小写的 Trae 模型 ID 和带下划线的公开 ID 会查不到已保存的设置；现在两侧统一使用同一个规范化 key，冷启动目录刷新不再丢掉 max 模式，请求了但模型不支持 max 模式时会记录日志而不是静默丢弃

## 0.2.26 - 2026-08-31

### English

- Rewrite the README opening around Qoder, WorkBuddy, and Trae CN Solo; center the title, badges, and hero; drop the Docker image badge
- Point README community discussion at LINUX DO, drop the CI badge, and keep documentation links on files that remain public
- Stop tracking maintainer design, plan, and provider notes; they stay local through `.gitignore`
- When the cross-provider model pool is disabled, reject bare model IDs instead of silently routing them to Qoder
- Enable the cross-provider model pool by default, persist the setting in SQLite, and move its toggle to System settings; upgrades initialize the missing setting to enabled and no migration is needed; `CROSS_PROVIDER_MODEL_POOL` is no longer used
- Remove the account card hover lift; the card no longer moves under the cursor
- Add-account wizard: the login-done message keeps the neutral surface and marks success with a check icon instead of an all-green box
- Logs page: default to last 1 hour instead of all time, darken the selected filter/chip color, and split search into exact model / account / request-ID dropdowns that load candidates and select before filtering (no more fuzzy free-text search)
- Sidebar status footer: show a skeleton while refreshing so it no longer flashes "degraded 0/0" on reload

### 中文

- README 开头改为列出 Qoder、WorkBuddy、Trae 国内 Solo，标题、徽章和 Hero 居中，并去掉 Docker Image 徽章
- README 增加 LINUX DO 社区入口，去掉 CI badge，文档链接只保留仍公开发布的文件
- 不再跟踪维护用的设计、计划和上游调研文档，改由 `.gitignore` 留在本地
- 关闭跨 Provider 模型池后拒绝 bare model ID，不再静默回落到 Qoder
- 跨 Provider 模型池默认开启，设置持久化到 SQLite，并移入「系统设置」开关；升级时缺失配置会直接写入开启状态，不需要新增迁移；不再使用 `CROSS_PROVIDER_MODEL_POOL` 环境变量
- 账号卡片去掉悬停上浮，悬停不再移动卡片
- 添加账号向导：登录完成提示保持中性底面，用绿色对勾标记成功，不再整盒变绿
- 日志页默认最近 1 小时，加深选中的筛选项颜色，并把模糊搜索拆成必须选后才过滤的模型 / 账号 / 请求 ID 下拉选择，不再支持自由文本搜索
- 侧边栏状态栏：刷新时显示骨架屏，不再闪成「降级 0/0」

## 0.2.24 - 2026-08-30

### English

- Add opt-in WorkBuddy daily check-in and token keepalive, plus console actions to check in now and refresh credits

### 中文

- WorkBuddy 支持账号级每日签到与 token 保活（默认关闭），控制台可立即签到并刷新积分

## 0.2.23 - 2026-08-30

### English

- Keep Signal Cyan on the C tip and scan only; primary buttons, focus, and selection use Charcoal Ink

### 中文

- Signal Cyan 只留在 C 的下唇和扫光；主按钮、聚焦和选中改走墨色

## 0.2.22 - 2026-08-30

### English

- Replace the bolt favicon and console mark with the single-rail C (cyan tip), and use the same path as a scan loader
- Lock the console accent to Signal Cyan so buttons, focus, and the C mark share one color instead of HeroUI blue
- Recolor README diagrams onto the same zinc-and-cyan palette, dropping leftover violet and ivory
- Match the account-card quota skeleton to the meter hairline instead of a capsule

### 中文

- 浏览器图标和控制台 mark 从闪电换成单轨 C（青尖），加载态沿同一条轨扫光
- 控制台强调色锁成 Signal Cyan，按钮、聚焦和 C 标共用一色，不再用 HeroUI 蓝
- README 示意图改走同一套锌灰 + 青，去掉残留的紫和象牙色
- 账号卡额度骨架圆角改成和额度细条一致，不再用胶囊

## 0.2.21 - 2026-08-30

### English

- Restyle the console on HeroUI v3 default tokens and compound primitives, replacing the ivory overlay, custom 32px chrome, and hand-rolled alerts, search, pagination, and empty states
- Lock console selection, radii, and type to one language: accent-soft selected chrome, Outfit + IBM Plex Mono, and ink-plus-cyan brand marks instead of mixed blue / white / violet fills
- Show HeroUI skeletons on first page load and on refresh or filter fetches, instead of a session spinner or leftover dashes
- Replace the hand-drawn Overview traffic SVG with a Recharts area chart on HeroUI tokens, and keep the GSAP line-draw entrance
- Strip leftover marketing chrome: login kickers, always-on status dots, colored left bars, and uppercase micro-labels
- Replace native Logs and Models filter dropdowns with compact HeroUI Select controls that match the 32px toolbar
- Add named client API keys with optional provider allowlists, and show the console administrator key on System
- Store an empty client-key provider allowlist as `[]` instead of JSON `null`
- Keep the v0.2.20 bytes for provider-model settings migration 007, and accept the later tab-indented checksum so existing databases can boot
- Explain on System when the host updater is missing, instead of showing the raw Unix socket error
- Show Trae Max-context and per-model reasoning controls, and WorkBuddy reasoning levels, on Models; persist those defaults and send them on chat

### 中文

- 控制台改走 HeroUI v3 默认 token 和复合原语，去掉象牙色覆盖、自定义 32px 控件，以及手写的提示、搜索、分页和空状态
- 选中态、圆角和字体锁成一套：选中走 accent-soft，界面 Outfit + IBM Plex Mono，品牌 mark 改为墨色闪电加青点，不再混用蓝 / 白 / 紫填充
- 第一次进入页面、刷新和打接口的筛选改走 HeroUI 骨架，不再用登录转圈或留下一排破折号
- 总览流量图改用 Recharts 面积图，颜色走 HeroUI token，入场仍用 GSAP 描线
- 去掉登录页 kicker、常亮状态点、彩色左边条和全大写微标签
- Logs 和模型页的筛选下拉改用紧凑 HeroUI Select，高度对齐 32px 工具栏
- 新增可限制供应商的客户端 API 密钥，并在系统页显示控制台管理员密钥
- 客户端密钥未限制供应商时写入 `[]`，不再存成 JSON `null`
- 007 供应商模型设置迁移保持 v0.2.20 原文，并接受后来误改缩进后的 checksum，已有数据库可以启动
- 系统页在没有宿主机更新器时说明更新条件，不再直接显示 Unix socket 报错
- 模型页为 Trae 显示 Max 上下文和推理强度，为 WorkBuddy 显示推理档位；默认值会保存，并在对话请求里带上

## 0.2.20 - 2026-08-29

### English

- Keep the v0.2.18 bytes for request-log provider migration 006, and accept the v0.2.19 tab-indented checksum so existing databases can boot

### 中文

- 006 请求日志供应商迁移保持 v0.2.18 原文，并接受 v0.2.19 误改缩进后的 checksum，已有数据库可以启动

## 0.2.19 - 2026-08-29

### English

- Make the Overview traffic chart taller so the area/line plot is readable
- Give the Overview traffic chart a full-width row so the series can use the console width
- Close the add-account dialog after a Trae callback succeeds, and give the pasted callback URL a fixed-height field
- Keep the Qoder context integer on Models, add a Trae Max-context switch, and map OpenAI reasoning fields onto each Trae model's allowed levels
- Size the Logs custom date-range field so start/end datetimes fit, and keep the calendar popover at calendar width

### 中文

- 加高概览流量图，面积折线更容易看清
- 概览流量图单独占一行，折线铺满控制台宽度
- Trae 提交回调成功后关闭添加账号弹窗，并把粘贴框改成固定高度的多行输入
- 模型页保留 Qoder 整数上下文；Trae 增加 Max 开关，并把 OpenAI 推理字段映射到该模型允许的档位
- 日志自定义时间范围加宽到能放下起止日期时间，日历弹层保持日历宽度

## 0.2.18 - 2026-08-29

### English

- Keep Trae pasted-callback errors as JSON instead of Cloudflare HTML, and keep Trae quota on the account card after refresh
- Treat Trae Solo `1005` / `4008` as quota, fail stream requests before OpenAI chunks when the first Solo event is an error, and send Trae's native catalog spelling (for example `DeepSeek-V4-Flash`)
- Show provider on Logs, Overview account pool, and account-page totals, and add Overview request share by provider family
- Replace the Overview traffic bars with a GSAP-drawn area/line chart, including success/error fill, axes, and hover readout

### 中文

- Trae 粘贴回调失败保持 JSON，不再被 Cloudflare HTML 盖住；刷新后 Trae 额度仍留在账号卡片上
- Trae Solo `1005` / `4008` 按套餐配额处理；流式若开头就是 Solo 错误则先失败再写 chunk；请求使用目录里的原始模型名（如 `DeepSeek-V4-Flash`）
- Logs、概览账号池和账号页统计显示供应商，概览增加按供应商家族的请求占比
- 概览流量图改为 GSAP 绘制的面积折线，带成功/失败分层、坐标轴和悬停读数

## 0.2.17 - 2026-08-29

### English

- Let Trae browser login finish by pasting the full `127.0.0.1/authorize` callback URL when the console cannot receive the loopback redirect
- Replace typed Logs custom time fields with a HeroUI date-range calendar, and filter the Models catalog by account provider instead of upstream `owned_by`
- Widen the account edit dialog and use HeroUI Form, NumberField, and Alert for name, concurrency, and priority
- After starting a managed update, keep Update now pending with a 10-second countdown, then reload the console

### 中文

- Trae 浏览器登录在控制台收不到本机回调时，可粘贴完整的 `127.0.0.1/authorize` 地址完成登录
- Logs 自定义时间改为 HeroUI 日历范围选择；Models 按账号供应商筛选，不再误用上游 `owned_by`
- 账号编辑弹窗加宽，名称、并发、优先级改用 HeroUI Form、NumberField 和 Alert
- 点击立即更新后按钮保持 loading，显示 10 秒倒计时，然后刷新控制台

## 0.2.16 - 2026-08-28

### English

- Add Trae CN Solo as an in-process account type (`provider=trae`, `region=cn`): browser login, credential import/export, live catalog, and OpenAI-compatible chat over `llm_utils_chat` / `solo_work_lite`, without spawning official `traecli`
- Fetch WorkBuddy Global model catalogs from `/v2/enterprises/personal/models` instead of the console OIDC page, and return catalog failures as 503 JSON so reverse proxies do not replace them with HTML
- After dropping caller system prompts, send WorkBuddy an empty leading system message so Global no longer rejects the request with `11128 first message is not system prompt`
- Show account-type skeletons in the add-account dialog until `/api/providers` returns, instead of flashing the default Qoder Global tile
- Show first-token time on Logs and label token counts as input / output
- Drop auth-type from account cards, keep cards mounted on refresh so quota and runtime meters can animate, and give those meters a very light same-hue gradient

### 中文

- 新增 Trae 国内 Solo 账号类型（`provider=trae`，`region=cn`）：支持浏览器登录、凭证导入导出、动态模型目录，以及走 `llm_utils_chat` / `solo_work_lite` 的 OpenAI 兼容对话，不启动官方 `traecli`
- WorkBuddy 国际版模型目录改打 `/v2/enterprises/personal/models`，不再走会 500 HTML 的 console 页面；目录失败改为 503 JSON，避免反代把错误换成 HTML 整页
- 丢弃调用方系统提示词后，仍给 WorkBuddy 补一条空的 system，避免国际版 `11128 first message is not system prompt`
- 添加账号弹窗等 `/api/providers` 返回后再显示类型，加载中用骨架屏，不再先闪默认 Qoder Global
- Logs 请求历史增加首字时间，Tokens 用小字标出输入 / 输出
- 账号卡片去掉认证方式；刷新时卡片保持挂载，额度和状态柱用 GSAP 过渡，色条加很浅的同色渐变

## 0.2.15 - 2026-08-28

### English

- Paginate runtime logs on Logs the same way as request history, newest first
- Filter the Models catalog by provider and paginate the list
- Fix WorkBuddy Global model catalogs: use the Global host from account region, accept CLI agent names besides exact `cli`, and surface catalog errors instead of an empty list
- Replace Access playground account and model tiles with compact side-by-side dropdowns
- Shorten the add-account dialog into two steps: pick type and name first, then sign in; hide concurrency and priority behind advanced options, and switch the type picker to a dropdown when more than six providers are registered
- Set account name, max concurrency, priority, and WorkBuddy drop-system-prompt before login, and edit name, concurrency, and priority on account cards
- Force-refresh account quotas when the console refresh button is used, bypassing the Qoder 15s quota cache
- Keep the Accounts toolbar visible while refresh is loading and show card skeletons instead of stale quota meters
- Drop the colored left accent on account cards; status stays on the chip and runtime meter

### 中文

- 日志页运行日志支持分页，交互与请求历史一致，最新记录在前
- 模型目录可按供应商筛选，并支持分页
- 修复 WorkBuddy 国际版模型目录：按账号区域打到国际站，识别不止精确 `cli` 的 CLI agent，失败时返回明确错误而不再显示空列表
- Access 调试台的账号和模型选择改为并排下拉框，避免模型过多时撑开页面
- 添加账号改为两步：先选类型和名称，再登录；并发和优先级收进高级选项，供应商超过 6 个时改用下拉
- 添加账号时先设置名称、最大并发、优先级，以及 WorkBuddy 的丢弃系统提示词；卡片上也可改名称、并发和优先级
- 控制台点刷新时强制重新拉取账号额度，绕过 Qoder 15 秒额度缓存
- 账号页刷新时保留顶部操作栏，卡片区域显示骨架屏，不再把旧额度留在画面上
- 去掉账号卡片左侧的彩色状态条，状态只留在 Chip 和运行短柱上

## 0.2.14 - 2026-08-28

### English

- Filter Access playground models to the selected account's live catalog instead of the global union
- Add an available-models button on account cards that opens that account's live catalog
- Show request volume, success rate, latency, tokens, and hourly traffic on Overview, with 1h / 24h / 7d windows from SQLite request history
- Shrink Accounts cards: denser identity row, a compact runtime meter, and a quota fill that animates remaining credits
- Show page skeletons again when the console refresh button is used, instead of leaving stale cards on screen
- Show a list skeleton on Logs while filters, pagination, or refresh are loading, without replacing the filter bar

### 中文

- Access 调试台选了账号后，模型列表改为该账号的实时目录，不再用全局并集
- 账号卡片增加「可用模型」按钮，弹窗查看该账号当前目录
- 概览页展示请求量、成功率、延迟、token 与按小时流量，时间窗口为 1 小时 / 24 小时 / 7 天，数据来自 SQLite 请求历史
- 账号卡片改为更紧凑的身份行：运行状态用短柱状指示，额度用填充条显示剩余量
- 控制台点刷新时重新显示骨架屏，不再把旧卡片留在页面上
- 日志页筛选、翻页或刷新时，下方列表显示骨架屏，筛选栏保持不动

## 0.2.13 - 2026-08-28

### English

- Upgrade the pinned Qoder CLIs from 1.1.27 to 1.1.32 (`@qoder-ai/qodercli` and `@qodercn-ai/qoderclicn`), with all worker compat needles re-verified against both new bundles
- Route chat by each account's live model catalog so a request like `hy3` only hits accounts that actually serve it; unknown models return `model_not_available` without cooling healthy accounts
- Render System page release notes as markdown for the current console language, in a box that grows with the text and scrolls when it overflows
- Paginate request history on Logs and filter by time range, account, model, stream mode, and error kind; runtime logs can also filter by account

### 中文

- 钉的 Qoder CLI 从 1.1.27 升级到 1.1.32（`@qoder-ai/qodercli` 与 `@qodercn-ai/qoderclicn`），worker 全部兼容 needles 已在两个新版 bundle 上重新验证
- 聊天按每个账号的动态模型目录选号，`hy3` 这类请求只会打到真正有该模型的账号；全池都没有时返回 `model_not_available`，不再给健康账号打冷却
- 控制台 System 页版本说明按当前语言渲染 markdown，说明框随文本增高，超出后可向下滚动
- 日志页请求历史支持分页，以及时间范围、账号、模型、流式模式、错误类型筛选；运行日志也可按账号筛选

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
