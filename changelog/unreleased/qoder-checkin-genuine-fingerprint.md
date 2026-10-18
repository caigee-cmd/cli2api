### English

- Qoder daily check-in now presents the **genuine desktop device fingerprint** read from the installed Qoder app's `umid/runtime-info` helper (`internal/providers/qoder/risk.go`), instead of only the PAT-derived identity. The campaign service only exposes the claimable daily-credit activity (`CLAIM_BENEFIT` / +100 Credits) to the official client's machine fingerprint and silently drops it for a derived one, which is why check-in previously reported "no campaign" / only saw the BOGO promo. When the helper is missing (headless server, other OS), the PAT-derived identity is still used as a graceful fallback.
- Global-region check-in now authenticates the campaign list/claim calls with the OpenAPI bearer (`/api/v1/jobToken/exchange`), matching the region's token requirement; CN keeps using the COSY `securityOauthToken`. A 401 now also invalidates the cached OpenAPI bearer before the single force-refresh retry, so a rejected token is re-exchanged rather than reused.
- The helper path can be overridden with `QODER_RUNTIME_INFO` for custom installs and tests.

### 中文

- Qoder 每日签到现在改用从本机安装的 Qoder App 自带的 `umid/runtime-info` 助手读取的**真实桌面设备指纹**（`internal/providers/qoder/risk.go`），而不再只用从 PAT 派生的身份。campaign 服务只对「官方客户端的机器指纹」暴露可领的每日积分活动（`CLAIM_BENEFIT` / +100 积分），对派生指纹会静默丢弃该活动——这正是此前签到报「无可签到活动」/只看到 BOGO 促销的原因。当助手不存在时（无桌面端的服务器、其他操作系统），仍回退到 PAT 派生身份，保证可用。
- 国际版签到改用 OpenAPI bearer（`/api/v1/jobToken/exchange`）来发起 campaign 列表/领取请求，符合该区的 token 要求；国内版继续使用 COSY 的 `securityOauthToken`。401 时现在会先清掉缓存的 OpenAPI bearer 再做一次强制刷新重试，避免复用被拒的 token。
- 助手路径可用 `QODER_RUNTIME_INFO` 覆盖，便于自定义安装与测试。
