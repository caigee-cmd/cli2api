### English

- Region-qualified model IDs: use `workbuddy-cn/glm-5.3-flash` or `workbuddy-gl/glm-5.3-flash` to route a request to CN or Global accounts respectively. Applies to all multi-region providers (WorkBuddy, Qoder).
- A bare provider prefix (`qoder/…`, `workbuddy/…`) keeps its existing behavior: no region is pinned and the pool auto-picks across regions, so international-only setups keep working unchanged. Bare model names with no prefix still auto-route across regions as before.
- New optional `-auto/` qualifier (`qoder-auto/…`, `workbuddy-auto/…`): an explicit spelling of the bare-prefix behavior — it pins the provider family while letting the pool pick the region, useful for cross-region load spreading within one provider. It reuses the pool's session-affinity latch so a route sticks to one region until it thins out.

### 中文

- 区域限定模型 ID：使用 `workbuddy-cn/glm-5.3-flash` 或 `workbuddy-gl/glm-5.3-flash` 可将请求分别路由到 CN 或 Global 账户。适用于所有多区域 provider（WorkBuddy、Qoder）。
- 不带区域限定的供应商前缀（`qoder/…`、`workbuddy/…`）行为不变：不锁定区域，由池跨区域自动选择，只有国际账号的部署不受影响。不带任何前缀的裸模型名也仍按原逻辑跨区域自动路由。
- 新增可选的 `-auto/` 限定形式（`qoder-auto/…`、`workbuddy-auto/…`）：与裸前缀行为一致的显式写法——固定供应商但不限定区域，由池自动选择区域，适合在单个 provider 内跨区域分摊负载。复用池的会话粘性机制，路由会粘在一个区域直到该区域候选不足才切换。
