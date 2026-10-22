### English

- **New** · Qoder: optional per-account traffic guard, off by default (`QODER_TRAFFIC_GUARD=on`; `QODER_TRAFFIC_GUARD_CONCURRENCY`, `QODER_TRAFFIC_GUARD_INTERVAL_MS`).
- **New** · WorkBuddy: `X-Private-Data` is configurable via `WORKBUDDY_PRIVATE_DATA=auto|true|false` (default `auto`).
- **New** · Qoder requests resolve the model name / display name to the account's catalog key; unknown models return `model_not_found`.

### 中文

- **新功能** · Qoder：新增可选的单账号流量守卫，默认关闭（`QODER_TRAFFIC_GUARD=on`；可调 `QODER_TRAFFIC_GUARD_CONCURRENCY`、`QODER_TRAFFIC_GUARD_INTERVAL_MS`）。
- **新功能** · WorkBuddy：`X-Private-Data` 可通过 `WORKBUDDY_PRIVATE_DATA=auto|true|false` 配置（默认 `auto`）。
- **新功能** · Qoder 按模型目录把模型名/显示名解析为账号网关 key，未知模型返回 `model_not_found`。
