### English

- Release the implicit region latch on cross-provider routes so a bare model shared by multiple providers can fail over to another supplier.
- Keep the Qoder global/CN region isolation for provider-scoped routes.
- Preserve the retry budget after the first pick instead of shrinking it to the pinned region's account count.
- Give each priority tier its own retry budget so sibling accounts cannot consume the whole budget before a lower-priority provider is reached.
- Apply account model aliases in the legacy worker request path.

### 中文

- 跨供应商裸模型路由不再被第一次选中的 region 隐式锁住，失败后可以切换到其他供应商。
- Qoder 等 provider-scoped 路由继续保持 global/CN 区域隔离。
- 首次选号后保留原重试预算，不再按 pinned region 的账号数量缩小 attempts。
- 每个 priority tier 使用独立重试预算，避免第一供应商的 sibling accounts 耗尽全局预算，导致低优先级供应商完全无法被尝试。
- legacy worker 请求路径也应用账号模型别名。
