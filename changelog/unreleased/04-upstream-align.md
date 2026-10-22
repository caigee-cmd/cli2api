### English

- **Upstream** · Qoder: one client version (1.1.32) across headers, signature and User-Agent (`QoderWork/…` CN, `Qoder/…` intl); streams add `cosy-machineos` / `cosy-machinehostname`. (#349)
- **Upstream** · WorkBuddy client versions: CLI 2.161.0, desktop 5.8.4 / 5.8.3 (CN), IDE 2.168.2, three-part desktop User-Agent. (#349)
- **Upstream** · WorkBuddy: chat 401 refreshes the token once and retries; TLS verification errors are no longer retried or failed over. (#348)
- **Upstream** · Qoder: 10605 queue errors return 429 + `Retry-After` without cooling the account; HTTP 430 shows "client version too low". (#348)
- **Upstream** · Devin: daily vs. weekly quota told apart; cooldown follows the window reset and lifts early once usage recovers. (#348)

### 中文

- **上游对齐** · Qoder：请求头、签名、User-Agent 统一客户端版本 1.1.32（国内 `QoderWork/…`、国际 `Qoder/…`）；流式补 `cosy-machineos` / `cosy-machinehostname`。（#349）
- **上游对齐** · WorkBuddy 客户端版本：CLI 2.161.0、桌面 5.8.4 / 5.8.3（国内）、IDE 2.168.2，桌面 UA 改为三段式。（#349）
- **上游对齐** · WorkBuddy：对话 401 先刷新 token 再重试同账号；TLS 证书错误不再重试或换号。（#348）
- **上游对齐** · Qoder：10605 排队返回 429 + `Retry-After`，不冷却账号；HTTP 430 提示“客户端版本过低”。（#348）
- **上游对齐** · Devin：区分日/周额度，按窗口重置时间冷却，额度恢复后提前解除。（#348）
