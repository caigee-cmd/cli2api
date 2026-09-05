# Changelog

User-facing notes for GitHub Releases and the console update page.
Write each change in both `### English` and `### 中文` under `## Unreleased`.

## Unreleased

### English

- Recommend Docker Compose as the supported install and managed-update path in the README and deployment guide

### 中文

- 在 README 与部署说明中明确推荐 Docker Compose 作为官方安装与托管更新路径

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
