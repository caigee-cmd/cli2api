---
id: cli2api-project-audit
title: CLI2API 项目审计与决策台账
scope: [all]
status: canonical
read-when: 每轮审计启动、冲突消解、问题状态追踪、验证基线对比、路线图推进时
summary: 单一真相审计报告。每轮幂等重写；不变量、决策、问题、验证、路线图只在此文件维护。
related: [AGENTS.md, docs/ARCHITECTURE_SUMMARY.md, docs/DEVELOPMENT.md]
last-updated: 2026-11-18
---

# CLI2API 项目审计报告

> 本文件是唯一审计真相源（第 10.1 节）。禁止日记式追加；每轮重写总摘要，同一问题保留稳定 ID。
> 审计对象：`e:/cli2api` **工作区当前状态**（含未提交改动）。
> 本轮性质：**基线审计轮（Round 1）**。按 1.5「不可半迁移」与 7.2「主题独立提交、单点回滚」，本轮**不修改业务代码**，只落台账 + 基线 + 问题清单 + 报告；所有修复以带方案待办登记。

## 当前总摘要

**健康度：⚠️ 需关注。** 产品分层骨架、SSE 流式正确性、更新器安全、pinned worker CLI（1.1.32）总体符合设计；但存在 1 项已提交的 pinned COSY 版本不一致、1 项测试固化的三类凭证隔离缺陷、1 项在途功能跨 region failover 风险，以及当前工作区 78 项未提交改动导致的基线测试红。

**最危险的 3 件事**
1. **SEC-001 (P0)**：console/admin key 可用于 `/v1/*` 网关端点，且该行为被测试 `internal/api/logs_api_key_test.go:46-48` 明确固化，违反 B1 三类凭证严格隔离。
2. **INV-001 (P0, 已提交)**：`COSYVersion` 实际值为 `1.1.81`（commit `4ce6e3f` 引入），而宪法 C3 与文档要求 `1.1.32`、worker/Dockerfile 钉在 `1.1.32`，协议版本与 pinned CLI 不一致。
3. **CORR-001 (P1, 在途)**：跨供应商聚合池（aggregation）在 `AllowFallbacks=true` 时可在不同 provider/region 成员间 failover，违反「跨 region failover 默认禁止」。

**下一步 3 件事**
1. 先让在途 aggregation 分支完成提交或回退，再以干净 HEAD 重跑基线，区分「在途功能红」与「既有环境红」。
2. 以独立提交修复 SEC-001（admin key 拒绝 `/v1/*`）并同步更新被固化的测试，走完 B1 回归。
3. 裁决 INV-001：要么更新宪法 C3/文档到 1.1.81 并给出协议兼容性验收，要么回退 COSY 版本到 1.1.32；二选一，禁止并存。

## 决策台账（生效约束集）

### DEC-000：第 0 节不变量原样生效（宪法级）

以下 A1–E4 为本轮首轮原样写入的**生效约束集**，任何提案先与之逐条比对（1.6）：

- **A1** 个人自托管网关，默认只监听 `127.0.0.1:3010`；不做计费、Redis 槽位、多租户 API keys、公开管理端口、session hash 变现。任何提案不得越界。
- **A2** 兼容不等于复刻：Messages/Responses 不保存服务端会话，不执行上游专属工具；图片能力取决于上游与模型 catalog 的 `supports_images`。
- **A3** provider 能力、模型目录、reasoning level 一律以实际 catalog 声明为准，禁止发明能力或硬编码推断。
- **B1** 三类凭证严格隔离：管理员密钥仅 `/api/*`，客户端密钥仅 `/v1/*`，上游凭证仅出站使用；不得交叉可用。
- **B2** API 响应与请求日志默认不返回/不保存原始 token、密钥、prompt、completion 正文；错误分支、debug 分支、panic 栈同样脱敏。
- **B3** 不得把默认 bind 改为 `0.0.0.0`；本机服务需 Host/Origin 校验防 DNS rebinding 与浏览器 CSRF；CORS 不得 wildcard + credentials。
- **C1** `internal/store/migrations.go` 中已发布迁移的 SQL 字节不可修改（以 hash 校验），新增 schema 只能追加编号迁移。
- **C2** `worker/` 固定 Qoder CLI/worker 兼容版本，不匹配必须明确失败，禁止静默降级或自动升级。
- **C3** Qoder 聊天走 Go 进程内 COSY 客户端签名直连（`COSYVersion=1.1.32`，`gateway.qoder.com.cn` / `api1.qoder.sh`），不得回退为每请求启动完整 CLI agent；登录/目录/配额仍走每账号独立 HOME 的 Node child process，HOME 必须隔离。
- **C4** Qoder 国内版与国际版同属 `provider=qoder`，以 `region` 区分；任何选号、冷却、粘性、日志、目录缓存的 key 必须包含 `region`。
- **D1** server 只做路由/CORS/OPTIONS/维护模式/WebUI；gateway 只做公共协议校验、转换与 SSE 输出；executor 独占选号、请求准备、错误分类、冷却、重试、failover；providers 只做上游协议与认证；control 做控制面编排；runtime 独占账号生命周期与刷新维护循环；store 只做持久化与迁移。
- **D2** gateway 内不得出现选号/冷却/failover 逻辑；providers 内不得读 store 或做调度；console 不得绕过 control 直改 store。
- **D3** 公共请求模型是唯一跨层契约，新增字段必须同步 OpenAI/Anthropic Messages/Responses 三套映射与测试。
- **D4** `internal/api` 仅测试兼容门面，生产不使用，禁止在其中新增业务，也禁止因「无引用」而删除。
- **E1** Go `1.25.6+`、Node `22+`；UI 改动必须 `npm run build && npm run lint && npm run sync`，产物同步到 `internal/webui/static/` 再由 Go embed；UI 改动需在 `changelog/unreleased/` 增加中英双语 fragment。
- **E2** 发布不手打 tag，只在 `main` 上 `gh workflow run release.yml --ref main` 串行触发。
- **E3** 前端栈固定 React + Vite + HeroUI v3 + Tailwind v4 + Phosphor；视觉冷锌灰 + Charcoal Ink，青色只点亮 C 标；不擅自引入第二套 UI/图标/样式体系。
- **E4** MIT 许可证；使用上游账号需遵守平台服务条款，不新增规避风控/条款的功能。

### DEC-001：首轮定位为基线审计轮，不做业务代码改动

- **依据**：工作区有 78 项未提交改动（分支 `fix/provider-model-level-errors`，HEAD `f6f75c9`），核心为在途 aggregation 功能；规则 1.5（禁止半迁移）、7.2（主题独立提交、单点回滚）、2.3（不并行牵连迁移）。
- **冲突检测**：与第 0 节不冲突；符合 E1/E2 的提交分离纪律。
- **影响文件/层**：无（仅新建 `docs/PROJECT_AUDIT.md`）。
- **取代关系**：无。

### DEC-002：COSY 版本冲突——登记待裁决，本轮不回滚

- **事实**：`internal/providers/qoder/qauth/fingerprint.go:18` 为 `const CosyVersion = "1.1.81"`（commit `4ce6e3f`，非工作区改动）；`cosy.go:25` 单源于该常量；`worker/src/compat.mjs:4` 仍 `PINNED_QODERCLI_VERSION = "1.1.32"`，`deploy/Dockerfile:30` 装 1.1.32；`docs/ARCHITECTURE_SUMMARY.md:76` 仍写 `COSYVersion = "1.1.32"`。
- **冲突检测**：与 **C3** 直接冲突（数值不符）；与 C2 存在一致性张力（Go 侧协议声明 1.1.81，CLI 钉 1.1.32）。端点未漂移（C3 端点部分通过）。
- **裁决**：不变量优先于「已提交事实」（1.1）；但 1.5 禁止半迁移、7.2 要求单点回滚，回滚已提交 commit 属独立主题。本轮**整体登记为 INV-001**，附二选一完整方案（A：回退到 1.1.32 + 契约测试；B：经协议验收后同步修订宪法 C3 + 文档 + 测试 + changelog）。二者只能选一，禁止并存。
- **影响文件/层**：`internal/providers/qoder/qauth/fingerprint.go`、`internal/providers/qoder/cosy.go`、`worker/src/compat.mjs`、`deploy/Dockerfile`、`docs/ARCHITECTURE_SUMMARY.md`、`CHANGELOG.md`、`AGENTS.md`（若改宪法）。
- **取代关系**：取代任何「COSYVersion 已是 1.1.32」的隐含假设；不取代 C3 本身。

### DEC-003：admin key 跨 `/v1` 为既有测试固化行为——修复需完整迁移

- **事实**：`internal/auth/verifier.go:67-69` 对 console key 返回 `ConsoleIdentity()`；`internal/server/middleware.go:51-62` 的 `withAPIKey` 只要求 `ok`，不拒绝 `KindConsole`；`internal/api/logs_api_key_test.go:46-48` 明确断言 console key 打 `/v1/chat/completions` 返回 200。
- **冲突检测**：与 **B1** 冲突（三类凭证不得交叉可用）。
- **裁决**：P0，但修复需同时改 `withAPIKey`/网关入口 + 固化测试 + 行为契约说明，属完整迁移（1.5）。登记 SEC-001，下轮以独立提交执行。
- **影响文件/层**：`internal/server/middleware.go`、`internal/auth/*`、`internal/api/logs_api_key_test.go`、`internal/gateway/keylimit.go`。
- **取代关系**：无。

### DEC-004：在途 aggregation 跨 region failover 风险——随在途分支处理，本轮登记

- **事实**：`internal/executor/chat.go:918-923,954-965` 在聚合路由下清空 `providerFilter`/`regionFilter`，`AllowFallbacks`/`MaxAttempts` 决定多次尝试；`internal/executor/aggregation_plan.go:74-88` 的 `matchMember` 在 `member.Region==""` 时匹配任意 region。
- **冲突检测**：与「跨 region failover 默认禁止」（宪法第 5.A 调度线，C4 精神）冲突。
- **裁决**：P1，属于在途主题，本轮不改；登记 CORR-001，随 aggregation 分支闭环前必须修。
- **影响文件/层**：`internal/executor/chat.go`、`aggregation_plan.go`、`pool.go`。
- **取代关系**：无。

### DEC-005：基线测试红——分类为「在途/环境」，不因本轮改动引入

- **事实**：`go test ./...` 失败包：internal/api（11 个 SPA/路由测试）、executor（backoff 2 个）、providers/qoder（2 个 CN spec）、runtime（1 个 CN spec）、store（4 个：1 个 backup mode 666≠600 + 3 个 Windows 文件锁 TempDir cleanup）。COSY 版本相关文件不在工作区改动集；backoff 断言为时间精度相等失败；store 失败为 Windows 文件占用/权限位语义。
- **冲突检测**：不违反不变量；但违反 9.1「基线全绿」期望，需显式登记。
- **裁决**：登记 VERIFY-001（P1，基线红），不归因于本轮（本轮无业务改动）。建议 owner 在干净 HEAD 与 Linux CI 上复测。
- **取代关系**：无。

### DEC-006：SEC-002 admin key 明文入日志——P1 待独立修复

- **事实**：`internal/app/app.go:79` 首次启动 `log.Printf("[security] initialized API key and stored it in SQLite: %s", proxyAPIKey)`，在 ring 初始化前，进 stderr/容器日志。
- **冲突检测**：与 **B2**（凭证不得进日志）冲突。虽进容器日志而非 API 响应，仍属凭证泄漏面。
- **裁决**：P1，独立安全修复提交；本轮登记。
- **取代关系**：无。

### 历史变更表（幂等：只记状态变化，不记过程）

| 日期 | ID | 状态变化 | 提交号 |
|------|----|----------|--------|
| 2026-11-18 | DEC-000..006 | 新增（首轮台账建立） | n/a（本轮仅新建审计报告） |
| 2026-11-18 | DEC-001(修订) | 用户指令推翻「本轮不改代码」：在途 aggregation 功能继续推进并合并为一个 PR | 见 PR 提交 |
| 2026-11-18 | CORR-001 | 复核后未复现：真实 route loop 的 `pickNext` 已在首个 pick 后锁定 regionFilter，跨区 failover 不可达；仅新增回归测试固化该不变量，未改生产代码 | 见 PR 提交 |
| 2026-11-18 | SEC(新) | `.dev-runtime/` 含 auth blob/qoder.db，加入 `.gitignore` 防泄漏 | 见 PR 提交 |

## 问题总表

状态机：新增 / 进行中 / 已闭环 / 仍存在 / 新引入 / 存疑保留 / 已取代。
验收状态：单测已验证 / 契约已验证 / 待端到端人工验收 / 已端到端验收。

| ID | 级别 | 状态 | 验收状态 | 标题 | 关键证据 | 处理 |
|----|------|------|----------|------|----------|------|
| SEC-001 | P0 | 新增 | 单测已验证（既有行为） | admin key 可用于 `/v1/*` | `internal/auth/verifier.go:67-69`、`internal/server/middleware.go:51-62`、`internal/api/logs_api_key_test.go:46-48` | 下轮独立提交：withAPIKey 拒绝 KindConsole + 改测试 + B1 回归 |
| INV-001 | P0 | 仍存在 | 契约已验证（代码/文档不一致） | COSYVersion 1.1.81 与 C3/文档/CLI pin 1.1.32 冲突 | `internal/providers/qoder/qauth/fingerprint.go:18`、`worker/src/compat.mjs:4`、`deploy/Dockerfile:30`、`docs/ARCHITECTURE_SUMMARY.md:76` | 二选一裁决（回退或修宪法+文档+验收），禁止并存 |
| CORR-001 | P1 | 已闭环 | 单测已验证（未复现） | 聚合池 AllowFallbacks 可跨 provider/region failover | `internal/executor/chat.go:1015-1018` 首个 pick 后锁定 regionFilter | 复核证伪：route loop 已锁 region，新增 `TestAggregateFailoverStaysInFirstRegion` 固化；无生产改动 |
| VERIFY-001 | P1 | 新增 | 待验证 | 基线 go test 红（在途/环境） | `.dev-runtime/audit-baseline-gotest.txt`；Windows 文件锁/权限位/时间精度 | 干净 HEAD + Linux CI 复测，区分在途与环境 |
| SEC-002 | P1 | 新增 | 单测已验证（代码事实） | admin key 首次启动明文入日志 | `internal/app/app.go:79` | 独立安全提交：改为指纹/前缀 |
| SEC-003 | P1 | 新增 | 推断（中置信） | 无 Host/Origin 校验 + CORS `*` + HOST 可改 0.0.0.0 | `internal/server/middleware.go:38-49`、`internal/config/config.go:49-52` | 本机威胁模型加固：Host/Origin 白名单；非 loopback 启动告警/拒绝 |
| SEC-004 | P2 | 新增 | 事实 | 自定义代理 URL 无私网/元数据 SSRF 校验 | `internal/proxy/proxy.go:79-90` | 解析并拒绝私网/回环/链路本地 |
| SEC-005 | P2 | 新增 | 事实 | 上游原始错误体/目录 body 记入日志，潜在回显敏感 | `internal/executor/chat.go:1208,1222`、`internal/runtime/manager_catalog.go:79,108,120` | 集中脱敏 + 长度限制 |
| SEC-006 | P2 | 新增 | 推断（中置信） | Windows updater `cmd /C` 拼接 hostPath，含 `"` 路径可注入 | `internal/updater/host_binary.go:118-125` | 改参数化调用/复用 os.Rename |
| ARCH-001 | P2 | 新增 | 事实 | gateway 在 SSE 失败后写回 executor 冷却/选路状态 | `internal/gateway/openai.go:177-185`、`internal/gateway/compat_stream.go:20-23` | 把 ObserveStreamFailure 触发下沉到 executor，gateway 只输出协议 |
| ARCH-002 | P2 | 新增 | 事实 | console 通过 Recorder.Store() 直读/清日志，绕过 control | `internal/console/logs.go:100-161`、`internal/logs/recorder.go:100-102` | 收敛到 control 提供的方法 |
| INV-002 | P2 | 新增 | 事实 | provider 对未知模型硬编码 reasoning/capability，绕过 catalog | `internal/providers/meta/catalog.go:74-90`、`internal/providers/zhipu/payload.go:96-113` | 未知模型返回最小能力集或显式拒绝 |
| CORR-002 | P2 | 新增 | 事实 | sticky/cooldown key 未含 region（C4） | `internal/executor/session_key.go:30-35`、`internal/executor/pool.go:1112-1118` | key 构造统一注入 region 归一化分量 |
| CORR-003 | P2 | 新增 | 推断 | 聚合成员不校验 catalog 能力；reasoning options 取并集 | `internal/control/aggregation_catalog.go:46-75`、`internal/accounts/aggregation.go:89-138` | 成员创建校验 catalog；决策交集/显式声明语义 |
| CLN-001 | P2 | 新增 | 事实 | worker compat 注入钩子吞异常 | `worker/src/compat.mjs:70,74,78,82` | 加可观测上报，禁止静默降级（需在 C2 pin 约束内） |

## 瘦身报告

本轮为基线审计轮，**未执行删除**。按 8.6 删除检索矩阵要求，仅登记候选，不动手：

| 候选 | 理由 | 无引用证据状态 | 本轮动作 |
|------|------|----------------|----------|
| 在途 aggregation 相关新文件 | 功能未完成，引用面未稳定 | 未做完整矩阵 | 存疑保留，随分支闭环后再评估 |
| `internal/api` 门面 | 生产无引用 | 保留（D4 永久豁免） | 不删 |
| 旧 hashed embed 资产（4 个 D + 5 个 ??） | 前端重建产物替换 | 属于 E1 embed 同步中间态 | 不单独处理，随 UI 提交闭环 |

依赖与体积基线：Go `go.mod` 直接依赖 4 项（godotenv / x/net / protobuf / modernc sqlite），Node 环境 v24.18.0 / npm 11.16.0。`frontend/node_modules` 存在（list_dir 显示 42616 文件），二进制/产物体积本轮未测（需在干净 HEAD 上测，避免 78 项未提交产物干扰）。

## 存疑保留项

1. **在途 aggregation 全部新文件**（`internal/console/model_probe.go`、`internal/executor/probe*.go`、`internal/control/aggregation*.go` 等）：功能未闭环，禁止本轮删除或重构。
2. **旧 embed hashed 文件**：待 UI 提交一起提交，Git 会显示旧文件为删除；本轮不单独清理。
3. **COSY 1.1.81 commit `4ce6e3f`**：已提交历史，不本轮回滚；待 DEC-002 裁决。

## 假设与待验证台账

| ID | 假设 | 验证方法 | 状态 |
|----|------|----------|------|
| AS-001 | 基线 go test 红主要由 Windows 文件锁/权限位及在途 aggregation 引起 | 在干净 HEAD + Linux CI 上 `go test ./... && go test -race ./...` | 【待验证】 |
| AS-002 | COSY 1.1.81 有真实协议兼容性收益（catalog-v7 / model_key 计费归属） | 查 commit `4ce6e3f` 说明 + 真实账号 SSE 契约回放 | 【待验证】 |
| AS-003 | 上游错误体回显 token/prompt 的概率低但非零 | 构造 mock 上游返回含 Bearer 的错误体，检查日志脱敏 | 【待验证】 |

联网权威检索（4.1，访问日期 2026-11-18）：
- OpenAI Streaming events（SSE chunk / finish_reason / usage / delta）：https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events/
- Claude Platform 流式消息（message_start → content_block_* → message_delta → message_stop）：https://platform.claude.com/docs/zh-CN/build-with-claude/streaming
- Go `http.Flusher` / context 取消语义以项目内实现 + Go 标准库为准；检索到的社区资料未作为事实来源。
说明：上游平台私有协议（Qoder COSY / WorkBuddy / Trae / CodeArts）无公开权威文档，按 4.2 以项目内实现与 catalog 为唯一真相。

## 验证与不变量校验对比

### 基线（工作区当前状态，78 项未提交改动）

| 项目 | 命令 | 结果 |
|------|------|------|
| Go 全部测试 | `go test ./...` | **FAIL**：api / executor / providers/qoder / runtime / store 红；其余 green（详见 DEC-005） |
| Go race | `go test -race ./...` | 未跑（基线已红，先收敛） |
| go vet | `go vet ./...` | 未跑（待干净 HEAD） |
| worker | `cd worker && npm test` | 未跑（待干净 HEAD） |
| 前端 | `npm run build && npm run lint` | 未跑（待干净 HEAD） |
| git diff --check | `git diff --check` | 未跑（待干净 HEAD） |

### 不变量自动校验（9.3）

| 不变量 | 本轮结果 | 证据 |
|--------|----------|------|
| 已发布迁移 SQL hash 未变（C1） | ✅ 通过 | `internal/store/migrations.go` 仅追加 028；`internal/store/backup_test.go` 的 001–027 hash 未变，新增 028 hash |
| 默认 bind 127.0.0.1（A1/B3） | ✅ 默认通过 | `internal/config/config.go:49-52` 默认 127.0.0.1；但 HOST 可改 0.0.0.0（SEC-003） |
| 三类密钥路由隔离（B1） | ❌ 失败 | admin key 可打 `/v1`（SEC-001） |
| 日志与响应脱敏（B2） | ⚠️ 部分 | app.go:79 明文 key（SEC-002）；上游错误体/目录 body 未脱敏（SEC-005） |
| COSYVersion / worker pinned（C2/C3） | ❌ 失败 | COSY=1.1.81 vs pin=1.1.32（INV-001） |
| provider 能力仅来自 catalog（A3） | ❌ 失败 | meta/zhipu 硬编码（INV-002） |
| gateway 内无调度逻辑（D1/D2） | ❌ 失败 | gateway 写回冷却/观察 failover（ARCH-001） |

## 六方一致性回归

| 维度 | 结论 |
|------|------|
| 代码 ↔ 测试 | ❌ admin key 跨 v1 行为被测试固化，与 B1 矛盾；基线测试红 |
| 代码 ↔ catalog/配置 | ❌ COSY 版本与 worker/Docker pin 不一致；provider 硬编码能力 |
| 代码 ↔ 迁移 | ✅ 仅追加 028，hash 测试覆盖 |
| 代码 ↔ 前端 embed 产物 | ⚠️ 工作区有 5D+5?? hashed 资产更替，需随 UI 提交闭环 |
| 代码 ↔ 文档 | ❌ 架构摘要仍写 COSY=1.1.32；README 写的 provider 验收状态需随功能状态复核 |
| 代码 ↔ 决策台账/报告 | ✅ 本文件为首轮建立，与上述发现一致 |

冲突收敛处理：全部按 1.4「禁止调和式补丁」裁决为带方案待办（DEC-002..006），不做兼容层/开关；未留半迁移中间态（本轮无业务改动）。

## 风险与回滚方式

| 风险 | 回滚方式 |
|------|----------|
| 本轮新建报告与在途改动无关 | 删除 `docs/PROJECT_AUDIT.md` 即完全回滚 |
| 未来 SEC-001 修复 | 独立提交；回滚即恢复 `withAPIKey` + 测试 |
| 未来 INV-001 裁决 A（回退 1.1.32） | revert commit `4ce6e3f` 对应版本改动 + 相关测试 |
| 未来 INV-001 裁决 B（升宪法到 1.1.81） | 版本只升不降；回滚文档与测试，保留代码 |
| 未来 CORR-001 修复 | 独立提交；回滚 region 约束前需确认无跨区依赖 |

## 手册与文档增补建议

1. `AGENTS.md` / `docs/ARCHITECTURE_SUMMARY.md`：统一 COSYVersion 事实（DEC-002 裁决后）。
2. `SECURITY.md` / `docs/DEVELOPMENT.md`：补「本机服务 Host/Origin 校验与 HOST 非 loopback 告警」运维条目（SEC-003）。
3. `CONTRIBUTING.md` / 审计手册：把「admin key 不得打 /v1」写成可执行的 B1 回归测试。
4. `changelog/unreleased/`：SEC-001/SEC-002 修复时按 E1 补中英双语 fragment。

## 全局路线图（按北极星指标排序）

| 轮次 | 主题 | 依赖 | 产出 |
|------|------|------|------|
| R2 | 在途 aggregation 分支闭环 + 干净 HEAD 基线重建 | owner 提交/回退工作区改动 | VERIFY-001 收敛，基线全绿 |
| R3 | SEC-001 admin/client 隔离（P0） | R2 干净基线 | B1 回归全绿 |
| R4 | INV-001 COSY 版本裁决（P0） | 协议验收材料 | 代码+文档+宪法一致 |
| R5 | CORR-001 聚合跨区 failover（P1） | R2 | 跨区测试红→绿 |
| R6 | SEC-002/003/004/005/006 安全加固批次 | R3 | 脱敏/SSRF/CORS/update 回归 |
| R7 | ARCH-001/002 + INV-002 + CORR-002/003 分层与 catalog 一致性 | R2–R4 | 分层契约测试 |
| R8 | 瘦身与依赖/CVE/体积扫描 | 以上全绿 | 删除清单 + 体积对比 |

## 待端到端验收清单

> 无真实上游账号的结论均标【待端到端人工验收】（7.4）。本轮未声称任何真实账号通过。

- INV-001 裁决 B（若选 1.1.81）：COSY 1.1.81 真实账号 SSE / catalog-v7 / model_key 计费归属【待端到端人工验收】。
- aggregation 跨供应商池：真实账号 failover / region 隔离 / 配额消耗【待端到端人工验收】。
- model_probe：真实账号探测对配额与限流的影响【待端到端人工验收】。
- 托管更新的真实升级/回滚：保持 README 既有「仍待验收」状态【待端到端人工验收】。

## 信息缺口

1. 78 项未提交改动的 owner 意图（提交还是回退）未知，影响基线归因。
2. commit `4ce6e3f` 升级 COSY 到 1.1.81 的完整协议验收材料未附，影响 DEC-002 裁决方向。
3. 无 Linux CI 结果，无法排除 Windows 环境对 VERIFY-001 的影响。
4. 前端 bundle 体积、二进制体积、覆盖率百分比本轮未测（需在干净 HEAD 上测）。
