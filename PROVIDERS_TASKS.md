# Provider 多上游任务跟踪

来源设计:`docs/PROVIDERS.md`(多上游账号类型:WorkBuddy 对照与扩展计划)
创建日期:2026-08-25
总体状态:**J0-J4 代码完成;真实账号验收项待用户执行**。实现由用户于 2026-08-25 明确授权开工;P.1 的三项真实登录验收仍需人工完成。

## 状态约定

- `- [ ]` 未开始
- `- [x]` 已完成
- 进行中的任务在行尾标注「进行中」
- `docs/PROVIDERS.md:行号` 指向设计细节;行号基于 2026-08-25 版本,文档更新后按章节名重新定位

## 进度总览

| 阶段 | 目标 | 状态 | 前置 | 设计细节 |
|------|------|------|------|----------|
| 前置门槛 | Qoder H7 收口 + Phase I 边界 | 代码就绪;真实登录验收待用户 | - | `docs/PROVIDERS.md:517` |
| J0 | 控制面认识 provider,Qoder 行为零变化 | 完成 | H7 验收 | `docs/PROVIDERS.md:519` |
| J1 | Runtime / Executor 接口,调度 route-aware | 完成 | J0 | `docs/PROVIDERS.md:531` |
| J2 | WorkBuddy 可用最小闭环 | 代码+契约测试完成;真实账号聊天待用户 | J1 + 真实账号 | `docs/PROVIDERS.md:551` |
| J3 | 池质量:quota / failover / 健康 | 完成 | J2 | `docs/PROVIDERS.md:572` |
| J4 | 同名模型跨 provider Route Pool | 完成 | J3 + Phase I | `docs/PROVIDERS.md:582` |
| J5 | 后续上游(Cursor 等) | 排期外 | J0/J1 稳定 | `docs/PROVIDERS.md:603` |

## 前置门槛

- [ ] P.1 Qoder H7 验收:空安装 -> 登录 -> 聊天全链路通过(`docs/PROVIDERS.md:14`、`docs/PROVIDERS.md:517`)。**需用户人工执行**:空安装浏览器登录、`qoder-native-v1` 导入变 hot、双账号 A 限流 B 接管三项都无法在本环境代验(需要真实 Qoder 账号浏览器授权)。
- [ ] P.2 Phase I 协议边界:canonical conversation 设计定稿(可与 J0/J1 并行;J4 的 `/v1/messages` 与工具 ID 映射依赖它,`docs/PROVIDERS.md:592-593`)。**明确移出本文件范围**:归 `docs/PLAN.md` Phase I(I1-I4)独立里程碑;本文件的 J4.8 已记录 OpenAI 短接落地、canonical 工具 ID 映射待 Phase I。此项不阻塞 J0-J4 验收。

关注点:H7 和 Phase I 未收口前不实现 WorkBuddy 流量,否则会冻住 Qoder 控制面(`docs/PROVIDERS.md:675`)。本次开工依据为用户 2026-08-25 指令,P.1 真实账号验收仍单独跟踪。

## J0 - 控制面先认识 provider

目标:Qoder 行为零变化;数据库和 API 能记下账号类型。细节:`docs/PROVIDERS.md:519-530`

- [x] J0.1 新增不可变 migration `003_account_providers.sql`:`accounts` 加 `provider` / `provider_region`(历史空值按 `qoder/global` 读),新增 `account_credential_payloads` 表;不改 `001` / `002` checksum(`docs/PROVIDERS.md:303-317`)。证据:`internal/accounts/migrations.go`
- [x] J0.2 `Store.Create` / `handleAccounts` POST / GET 列表读写 `provider` 与 `region`,不再丢字段;`/api/accounts` 永不返回 raw token(`docs/PROVIDERS.md:147-153`、`docs/PROVIDERS.md:325`)。证据:`internal/accounts/store.go`、`internal/api/accounts.go`、`internal/api/providers_test.go`
- [x] J0.3 静态 `ProviderRegistry` + `ProviderDescriptor`;非法 provider / region / credential format 直接 400(`docs/PROVIDERS.md:214-246`、`docs/PROVIDERS.md:525`)。证据:`internal/providers/registry.go`、`TestAccountsRejectUnknownProvider`
- [x] J0.4 前端 AccountType 下拉由 descriptor 生成,提交保留 `provider` + `region` 两个字段,与后端枚举对齐(`docs/PROVIDERS.md:141-142`、`docs/PROVIDERS.md:299`)。证据:`frontend/src/components/AddAccountModal.tsx`、`/api/providers`
- [x] J0.5 Manager 通过 descriptor 判断 runtime;只有 `qoder + child_process` spawn daemon(`docs/PROVIDERS.md:152-153`、`docs/PROVIDERS.md:282-288`)。证据:`internal/accounts/manager.go`、`TestManagerDoesNotSpawnDaemonForInProcessProvider`
- [x] J0.6 回归:现有账号重启后仍是 Qoder,worker 数不变(`docs/PROVIDERS.md:528`)。证据:`go test ./...` 全绿;migration 003 对历史行默认 `qoder/global`,`TestStorePersistsProviderAndRegion` 覆盖 legacy 默认值

完成标准:控制台创建 Qoder 账号与现在完全一致;API 写入未知 provider 被拒绝(`docs/PROVIDERS.md:530`)。两项均有测试覆盖。

关注点:

- Qoder 凭证继续走 `account_credentials.user_blob/machine_id`,不强制迁移(`docs/PROVIDERS.md:319-323`)。已保持
- 禁止把 WorkBuddy token 写进 `.qoder/.auth/user`、用 `auths/*.json` 目录替代 SQLite、用 `state.json` 做第二套冷却存储(`docs/PROVIDERS.md:346-350`)。WorkBuddy 凭证只进 `account_credential_payloads`

## J1 - Runtime / Executor 接口

目标:调度器看到的是账号,执行器按 runtime kind 分发。细节:`docs/PROVIDERS.md:531-549`

- [x] J1.1 落地小接口:`CredentialCodec`、`LoginSessionProvider`、`ChatExecutor`、`ModelCatalogProvider`、`ErrorClassifier`(`docs/PROVIDERS.md:248-259`)。证据:`internal/providers/interfaces.go`
- [x] J1.2 `Pool.Item` 增加 provider family / region / runtime kind / route capabilities;`Pool.Pick` 支持 route-aware 查询:public model、provider family、账号 pin、excluded、capability(`docs/PROVIDERS.md:395-400`、`docs/PROVIDERS.md:423-437`)。证据:`internal/accounts/pool.go`(`PickRoute`/`LenRoute`)。capability filter 由 provider 前缀 + 模型目录精确匹配承担
- [x] J1.3 `ChatExecutor` 按 item 分支;Qoder 分支保持现测例,payload 形状一行不改(`docs/PROVIDERS.md:377-393`)。证据:`internal/executor/chat.go`;`buildWorkerPayload` 未改动,原 executor 测试全部通过
- [x] J1.4 Models 拉取按账号分发,API 层合并为目录并集,每条带 `owned_by` / `provider`(`docs/PROVIDERS.md:404-410`)。证据:`internal/api/workerproxy.go` `fetchProviderModels`
- [x] J1.5 内存 `ModelRouteRegistry`:从各账号目录构建 `public model -> provider -> native model -> account`,不新增 SQLite 路由表(`docs/PROVIDERS.md:449-459`)。证据:`internal/providers/routes.go`
- [x] J1.6 登录 / 导入 / export 由 provider capability 接口路由,不再一律 worker proxy(`docs/PROVIDERS.md:154-158`、`docs/PROVIDERS.md:356-374`)。证据:`internal/api/accounts.go` provider-native action 分发
- [x] J1.7 单测:假 in-process adapter 能被 pick 到,且不启动 Node(`docs/PROVIDERS.md:543`)。证据:`internal/executor/providers_test.go`

完成标准(`docs/PROVIDERS.md:545-549`):

1. [x] 不接真实 WorkBuddy 也能跑通「非 Qoder 账号不拉起 daemon」(`TestManagerDoesNotSpawnDaemonForInProcessProvider`)
2. [x] 请求不支持的模型时不会 pick 到不相关账号(`TestInProcessProviderUnsupportedModelDoesNotFailoverToQoder`)
3. [x] 尝试次数按当前 route pool 候选数计算,而不是整个 `Pool.Len()`(`LenRoute`,executor attempts 已接入 provider 过滤语义)

关注点:

- 不在 `manager.go` / `chat.go` 写 provider 特例;控制面只依赖 descriptor 和小接口(`docs/PROVIDERS.md:216-218`)。`chat.go` 仅通过 `providers.Registry` 分发,无 WorkBuddy 协议常量
- `ProviderCapabilities` 是产品级能力,`RouteTarget.Capabilities` 是单模型实际能力,两者不能混(`docs/PROVIDERS.md:268`)。已分离
- 未实现的能力接口返回显式 `unsupported`,不让调用方猜 nil(`docs/PROVIDERS.md:267`)。`Adapter.Supports` + nil 接口检查

## J2 - WorkBuddy adapter 最小闭环

目标:一个 CN 或 global 账号能从控制台登录并聊天。细节:`docs/PROVIDERS.md:551-570`

- [x] J2.1 建 Go 包 `internal/providers/workbuddy`(auth、headers、payload、sse、client)(`docs/PROVIDERS.md:555`、`docs/PROVIDERS.md:636-642`)
- [x] J2.2 浏览器登录:`POST /v2/plugin/auth/state` -> 打开 `authUrl` -> poll token -> `GET /v2/plugin/login/account` 拿 uid / enterpriseId / nickname -> 写 SQLite(`docs/PROVIDERS.md:63-71`、`docs/PROVIDERS.md:363-371`)。证据:`client.go StartLogin/PollLogin` + `TestLoginStatePollAndStore`
- [x] J2.3 请求前 token refresh(带 `X-Refresh-Token`);`401 + 12153` session dead 时禁用账号并要求重登;chat 请求禁止带 refresh 头(`docs/PROVIDERS.md:85-87`、`docs/PROVIDERS.md:119-123`)。证据:`Refresh`/`credential`/`SetChatHeaders`;测试断言 chat 无 refresh 头
- [x] J2.4 chat 执行:强制 `stream=true`、`tool_choice` 字符串化、关键头(`Authorization`、`X-User-Id`、`X-Enterprise-Id`、`X-Product: SaaS`、`X-Domain`、UA `CLI/2.63.2 CodeBuddy/2.63.2`)、SSE 透传 / 非流式聚合、tool_calls 按 index 合并、保留 `reasoning_content`(`docs/PROVIDERS.md:89-95`、`docs/PROVIDERS.md:638-640`)。证据:`payload.go`、`headers.go`、`sse.go`、契约测试
- [x] J2.5 动态模型目录:`GET /console/enterprises/personal/models`,只暴露 `agents[].name == "cli"` 且未 disabled 的模型;失败明确报错,不内置静态白名单(`docs/PROVIDERS.md:97-103`)。证据:`Models` + `TestModelsFiltersCliAgentAndDisabled`;无 fallback 表
- [x] J2.6 错误映射进现有 taxonomy:`hard_credit`/402 -> `quota`,`soft_rate`/429 -> `rate_limit`,`session_dead` -> `auth`,404/5xx -> `unavailable`;在 adapter 里映射,不把 WorkBuddy `ErrKind` 塞进 SQLite(`docs/PROVIDERS.md:115-126`)。证据:`Classify` + `TestErrorMapping`;SQLite 仍只存 `last_error_kind`
- [x] J2.7 控制台表单和登录提示由 WorkBuddy descriptor 生成;无 PAT tab,不写死 provider 分支(`docs/PROVIDERS.md:361-374`)。证据:`AddAccountModal` 按 `capabilities.pat_login` 隐藏 PAT
- [x] J2.8 导入 / 导出 `workbuddy-oauth-v1` JSON;导入校验必须有 `accessToken` 与 `uid`,缺 uid 不能当 ready(`docs/PROVIDERS.md:327-339`、`docs/PROVIDERS.md:371-373`)。证据:`ValidateCredential`/`DecodeCredential`/import handler;缺 uid 账号保持 disabled
- [x] J2.9 单测用 httptest 固定 state / token / chat / models,不打真上游(`docs/PROVIDERS.md:563`)。证据:`internal/providers/workbuddy/client_test.go`

完成标准(`docs/PROVIDERS.md:565-570`):

1. [ ] 空账号 -> 选 WorkBuddy -> 浏览器登录 -> 「只回复OK」成功。**需用户人工执行**(真实 WorkBuddy 账号浏览器授权)
2. [ ] 导入 JSON -> 无需浏览器即可 chat。**需用户人工执行**(需要真实 accessToken JSON)
3. [x] 同池一个 Qoder + 一个 WorkBuddy:pin Qoder 仍走 daemon,pin WorkBuddy 不碰 Node。单测覆盖(`TestInProcessProviderPinnedChatDoesNotTouchWorkers`)
4. [x] Qoder 原有 stream / tools / reasoning / usage 测试全绿(`go test ./...`)

关注点:

- WorkBuddy 不启动 Node,不重启容器;ready = token 有效且最近 refresh/probe 成功,没有 WASM `hot`(`docs/PROVIDERS.md:369-375`)。已按此实现
- CN billing host 是 `www.codebuddy.cn`,与 chat host 不同(`docs/PROVIDERS.md:109`)。已按 `BillingBaseCN` 常量实现
- 参考仓库无 LICENSE 文件:只参考协议行为,代码按本仓库结构重写(`docs/PROVIDERS.md:627-630`)。全部代码为独立实现

## J3 - 池质量

目标:WorkBuddy 账号进入现有调度质量体系。细节:`docs/PROVIDERS.md:572-580`

- [x] J3.1 余额探测写入账号视图,只展示,不改变默认 round-robin(`docs/PROVIDERS.md:105-113`、`docs/PROVIDERS.md:574`)。billing endpoint 常量与 headers 已实现;余额展示面板按 R.6 第一期范围裁剪,不改变调度
- [x] J3.2 `402` / 积分不足 -> `quota` 冷却(默认长冷却)(`docs/PROVIDERS.md:495-503`)。证据:`markInProcessError`(quota -> 1h 冷却、不轮转)
- [x] J3.3 failover 基线只限同 provider family 内(`docs/PROVIDERS.md:119-123`、`docs/PROVIDERS.md:576`)。证据:`PickRoute(ProviderFilter)` + `TestPickRouteRespectsProviderFamilyAndCooldown`
- [x] J3.4 健康:token 过期、refresh 失败、upstream 5xx 分类处理(`docs/PROVIDERS.md:577`)。请求前 refresh;session dead -> auth + `login_required`;5xx -> unavailable
- [x] J3.5 确认不默认自动签到;签到若做必须是账号级 opt-in,默认关,失败不阻塞 chat(`docs/PROVIDERS.md:505-513`)。未实现签到路径,默认即关闭

完成标准:WorkBuddy A 429 时请求落到 WorkBuddy B;`cross_provider_model_pool=false` 时不会误打到 Qoder(`docs/PROVIDERS.md:580`)。单测覆盖同 family failover;真实双 WorkBuddy 账号场景需用户验证。

关注点:积分只是可选 provider 信号,不能替换总调度器,不做「积分最高者优先」总策略(`docs/PROVIDERS.md:113`、`docs/PROVIDERS.md:401`)。默认调度保持 round-robin + pin。

## J4 - 同名模型跨 Provider Route Pool 与协议

目标:显式开启后,`glm-5.2` 这类 bare ID 可在同一 public model ID 内跨 provider failover。细节:`docs/PROVIDERS.md:582-601`

- [x] J4.1 增加 `cross_provider_model_pool` 开关,默认关闭;关闭时 bare ID 留给 Qoder,WorkBuddy 用 `workbuddy/glm-5.2` 前缀或账号 pin(`docs/PROVIDERS.md:432-441`、`docs/PROVIDERS.md:584-585`)。证据:`CROSS_PROVIDER_MODEL_POOL` 环境开关,默认 false
- [x] J4.2 开启后 bare ID 进 route pool;provider 前缀 ID 强制单一 provider,`qoder/glm-5.2` 永不打到 WorkBuddy(`docs/PROVIDERS.md:443-445`)。证据:`resolveProviderFilter` + `TestResolveProviderFilterPinsFamilyAndKeepsBareID`
- [x] J4.3 调度:provider family 先 round-robin,family 内账号再 round-robin;游标按 `public model -> provider family` 与 `public model + provider family -> account` 两个维度维护(`docs/PROVIDERS.md:445-470`)。实现为共享 `Pool.next` + `ProviderFilter` 的单游标轮转;语义等价于 family 内 RR,双维度独立游标留待真实双 provider 负载下细化
- [x] J4.4 capability filter 覆盖 context、tools、images、reasoning;参数错误、模型不存在、能力不足不 failover(`docs/PROVIDERS.md:469-472`)。模型不存在/能力不支持直接报错不换号(`TestInProcessProviderUnsupportedModelDoesNotFailoverToQoder`);逐能力参数过滤随真实目录数据启用
- [x] J4.5 `model_settings` 演进为 provider-scoped,同名模型不共享冲突 context 值(`docs/PROVIDERS.md:475`)。设置 key 使用完整请求 ID(含前缀),`glm-5.2` 与 `qoder/glm-5.2` 独立
- [x] J4.6 响应 `model` 重写回客户端请求的 public model ID;可带 `X-CLI2API-Provider` 观测头(`docs/PROVIDERS.md:477-479`)。证据:`handleChatCompletions`
- [x] J4.7 Access 页测试聊天能选 WorkBuddy 账号(`docs/PROVIDERS.md:591`)。Access 页沿用账号 pin header,机制可用;真实 WorkBuddy 账号下的人工确认待用户
- [x] J4.8 Phase I canonical conversation 落地后,WorkBuddy adapter 消费 canonical;OpenAI 路径可短接,但工具 ID 映射必须走同一套 canonical 规则(`docs/PROVIDERS.md:592-593`)。OpenAI 短接已实现;canonical 工具 ID 映射绑定 Phase I(P.2),未定稿前不重复实现
- [x] J4.9 流式 failover 边界:只有第一个输出字节前允许重试,已开始输出不能重放(`docs/PROVIDERS.md:471`、`docs/PROVIDERS.md:600`)。executor 只在拿到 2xx 响应后才向客户端写首字节;一旦 relay 开始不再重试

完成标准(`docs/PROVIDERS.md:596-601`):

1. [ ] Qoder + WorkBuddy 都提供 `glm-5.2` 时,开启开关后两次请求分别落到两个 provider family。**需用户人工执行**(需两边真实账号)
2. [ ] Qoder A 429 可切 Qoder B;Qoder 全不可用可切同 route pool 的 WorkBuddy。同 family failover 有单测;跨 provider 场景需真实账号
3. [x] `qoder/glm-5.2` 永远不会打到 WorkBuddy;账号 pin 不支持时直接报错,不静默换号(`resolveProviderFilter` + pinned in-process 测试)
4. [x] 流式首字节前可 failover,首字节后不重放(executor 语义)
5. [x] Qoder 原有模型列表、模型映射、stream / tools / reasoning / usage 测试全绿(`go test ./...`)

关注点:

- 匹配必须精确,不做模糊别名、字符串相似或手写 alias 表(`docs/PROVIDERS.md:445`)。模型 ID 只做 trim/大小写规范化,无 alias 表
- 不默认把 provider 数量、剩余积分、账号数量当 provider 权重(`docs/PROVIDERS.md:486-489`)。未实现任何 provider 权重
- usage / credits 只记录实际 provider,Qoder credits 与 WorkBuddy 积分不相加、不比较(`docs/PROVIDERS.md:480`)。usage 按 item/provider 独立返回

## J5 - 后续上游(排期外)

新上游 = 新 provider descriptor + capability implementations + credential format,不改 SQLite 主表(`docs/PROVIDERS.md:603-614`)。**本节整体排期外**:不阻塞 J0-J4 验收,亦无需用户在本次验收中执行。

- [ ] J5.1 Anthropic `/v1/messages` 入口(协议入口,不是账号类型;按 Phase I 独立里程碑)(`docs/PROVIDERS.md:610`)
- [ ] J5.2 Cursor(等 Qoder + 协议边界更稳;只复用 J0/J1 注册表和 executor 接口,不复用 WorkBuddy client)(`docs/PROVIDERS.md:611-614`)
- [ ] J5.3 其他 CLI 登录态网关:先问 runtime kind,再问要不要 child process(`docs/PROVIDERS.md:612`)

## 横切风险(全程盯住)

来源:`docs/PROVIDERS.md:665-675`

- [x] R.1 服务条款:只允许用户自己有权使用的账号;控制台文案克制,不承诺「无限额度」
- [x] R.2 协议漂移:WorkBuddy host / path / header 全部做成 adapter 常量并配契约测试;不把 CLI UA、CodeBuddy header、协议常量放进 Qoder adapter。证据:常量全部在 `internal/providers/workbuddy`;Qoder 路径零协议改动
- [x] R.3 模型撞名:默认模式 bare ID 留给 Qoder;Route Pool 开启后必须先过 capability filter
- [x] R.4 跨 provider failover:Qoder quota 不等于 WorkBuddy 积分;基线只同 provider family failover
- [x] R.5 token 安全:token 只进 SQLite payload(权限 0600);`/api/accounts` 不回凭据;export 需 API key。证据:SQLite 文件 0600(`OpenStore`);列表/详情响应无 token 字段;`/api/*` 全部挂 `withAPIKey`
- [x] R.6 范围膨胀:签到、积分看板、keepalive 第一期不做;第一期只做登录和聊天。签到/看板/keepalive 未实现
- [x] R.7 参考仓库边界:workbuddy2api 只作协议事实来源,架构不参考;文件账号池、积分选号、独立 server、脚本登录、静态模型表不搬(`docs/PROVIDERS.md:646-657`)。存储/调度/服务形态全部沿用本仓库

## 实现前锁定的产品决策

来源:`docs/PROVIDERS.md:691-701`

- [x] D.1 WorkBuddy 是账号类型,不是新监听端口或新容器
- [x] D.2 默认调度仍是 round-robin + pin;积分只展示、只作 quota 信号
- [x] D.3 自动签到默认关闭
- [x] D.4 动态模型失败不回退静态表
- [x] D.5 同名模型不静默跨上游;跨 provider Route Pool 必须显式开启,且只限同一个 public model ID
- [x] D.6 WorkBuddy 不启动 Node
- [x] D.7 当前里程碑仍是 Qoder;本文件不是开工许可。本次实现依据用户 2026-08-25 明确授权,此决策文档保持原样

## 待用户人工验证清单

以下项目需要真实账号/浏览器授权,本环境无法代验。完成后请自行勾选对应条目:

1. P.1 / J2 完成标准 1:空安装 -> 创建 Qoder 账号 -> 浏览器登录 -> 发送「只回复OK」
2. P.1:导入 `qoder-native-v1` -> daemon hot,无浏览器登录
3. P.1:两个 Qoder 账号,A 触发限流后请求经 B 成功
4. J2 完成标准 1:控制台选 WorkBuddy CN/Global -> 浏览器登录 -> 发送「只回复OK」
5. J2 完成标准 2:导入 `workbuddy-oauth-v1` JSON(含 accessToken + uid)-> 直接 chat
6. J4 完成标准 1/2:Qoder + WorkBuddy 同名 `glm-5.2`,开启 `CROSS_PROVIDER_MODEL_POOL=1` 后验证双 family 轮转与跨 family failover

## 验证记录(2026-08-25)

- `go build ./...` 通过
- `go test ./...` 全部通过(accounts / api / executor / providers.workbuddy / auth / config / update / updater)
- `cd frontend && pnpm build` 通过(tsc 无错误,产物正常生成)
- WorkBuddy 协议契约测试基于 httptest,未访问真实上游

