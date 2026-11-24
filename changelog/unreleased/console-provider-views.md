### English

- Provider-wide settings now live in a modal on the Models page. Account editing keeps only name, concurrency, and priority; provider policy owns floors, proxy, system-prompt handling, aliases, enabled models, and check-in time. Saving provider settings closes immediately and re-filters the cached catalog in the background instead of waiting for an upstream refresh.
- The Models page groups models into one card per connected provider and region, with a model count in the header. The provider filter lists only connected provider/region pairs, defaults to the first one, and the redundant "Model catalog" intro header is gone. A provider restricted to a selected model list no longer shows `auto`.
- The cross-provider model editor keeps its header and footer visible and scrolls the form body on short screens. Members are configured by provider, region, and model only; the account is chosen automatically from the provider account pool instead of being pinned in the editor. The editor lists only connected provider/region combinations and models enabled by that provider's policy.
- Account cards show the upstream check-in state (`already`, `pending`, `skipped`, or `unknown`) with the last sync time, and scheduled check-in defaults to automatic mode based on the upstream window.

### 中文

- 供应商级设置改为模型页上的统一弹窗。账号编辑只保留名称、并发和优先级；积分/百分比保底、代理、系统提示词处理、别名、可用模型和签到时间由供应商策略统一管理。保存供应商设置后弹窗立即关闭，已缓存目录在后台重新过滤，不再等待上游刷新。
- 模型页按已接入的供应商与区域分组，每个供应商一张卡片，标题显示模型数量。供应商筛选只列出有账号接入的供应商/区域并默认选中第一个，顶部冗余的“模型目录”说明栏已移除。限制为勾选模型的供应商不再显示 `auto`。
- 跨 Provider 模型编辑器在矮屏下固定标题和底部操作区、让表单内容独立滚动。成员只按供应商、区域和模型配置，账号由供应商账号池自动选择，不再在编辑器里指定单个账号；编辑器只列出已接入的供应商/区域和该供应商策略启用的模型。
- 账号卡片显示上游签到状态（已签到、未签到、未开放或状态未知）和最近同步时间；自动签到默认按上游窗口自动排期。
