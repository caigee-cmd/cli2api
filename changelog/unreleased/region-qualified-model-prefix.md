### English

- Region-qualified model IDs: use `workbuddy-cn/glm-5.3-flash` or `workbuddy-gl/glm-5.3-flash` to route a request to CN or Global accounts respectively. Applies to all multi-region providers (WorkBuddy, Qoder).
- A bare provider prefix (`qoder/…`, `workbuddy/…`) now defaults to the CN region instead of letting the pool auto-pick. Use the qualified form (`qoder-gl/…`, `workbuddy-gl/…`) to reach Global accounts explicitly. Bare model names with no prefix still auto-route across regions as before.
- New `-auto/` qualifier (`qoder-auto/…`, `workbuddy-auto/…`) pins the provider family while letting the pool pick the region — useful for cross-region load spreading within one provider. It reuses the pool's session-affinity latch so a route sticks to one region until it thins out.

### 中文

- 区域限定模型 ID：使用 `workbuddy-cn/glm-5.3-flash` 或 `workbuddy-gl/glm-5.3-flash` 可将请求分别路由到 CN 或 Global 账户。适用于所有多区域 provider（WorkBuddy、Qoder）。
- 不带区域限定的供应商前缀（`qoder/…`、`workbuddy/…`）现在默认路由到 CN 区域，而非由池自动分配。如需 Global 账户请显式使用 `qoder-gl/…`、`workbuddy-gl/…`。不带任何前缀的裸模型名仍按原逻辑跨区域自动路由。
- 新增 `-auto/` 限定形式（`qoder-auto/…`、`workbuddy-auto/…`）：固定供应商但不限定区域，由池自动选择区域，适合在单个 provider 内跨区域分摊负载。复用池的会话粘性机制，路由会粘在一个区域直到该区域候选不足才切换。
