### English

- Add opt-in earliest-expiry-first (FEFO) account routing. It ranks accounts only inside the same provider family by their nearest spendable credit-package expiry, rotates accounts expiring on the same UTC+8 day, and falls back to round-robin when a provider has no package dates. Existing round-robin, weighted round-robin, fill-first, affinity, pinning, region/provider gates, cooldowns, and failover are unchanged.
- Refresh a FEFO account's quota after each completed response or stream (coalesced per account) and re-probe accounts whose trusted packages all expired, so newly granted or spent credits change the next selection without waiting for a manual refresh.
- Expose WorkBuddy package expiry as FEFO-eligible only for personal identities with a complete, paginated result. Enterprise/shared identities, full result pages, undated packages, and other providers' reset windows or token lifetimes never invent a priority.
- Add the FEFO option to System → Account routing strategy and document that Qoder, Trae, Devin, Command Code, and Codex do not currently provide per-package credit expiry, so their accounts stay in the unknown-date cohort.

### 中文

- 新增可选的同供应商最早到期优先（FEFO）调度：只在同一供应商内按最近可消费积分包到期日排序，同一 UTC+8 自然日到期的账号公平轮询；供应商没有积分包日期时回退轮询。原有轮询、加权轮询、顺序填满、会话粘性、固定账号、地域/供应商限制、冷却与故障切换保持不变。
- 每次响应或流结束后异步刷新该账号额度（按账号合并请求），并重新探测积分包已全部过期的账号，让新增或已消耗的积分立即影响下一次选择，无需手动刷新。
- 仅 WorkBuddy 个人身份且分页结果完整的积分包参与 FEFO 优先级；企业/共享身份、满 100 条的未确认列表、无日期包，以及其他供应商的重置窗口或令牌有效期都不会被伪造成到期优先级。
- 在「系统 → 账号调度策略」增加 FEFO 选项，并说明 Qoder、Trae、Devin、Command Code、Codex 当前不提供逐包积分到期信息，其账号保持未知日期分组。
