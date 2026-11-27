### English

- Trae renews a rejected access token once per request and retries on the same account, and a transient renewal failure no longer marks the account "needs login".
- A successful credential renewal (Trae, WorkBuddy) clears a stale "needs login" status, so the card stops contradicting the live routing state.
- WorkBuddy session keepalive covers every enabled account, not only check-in opt-in accounts.
- Account cards resolve status in one documented order: disabled, recovering, needs login, quota exhausted, cooling, starting, ready, unavailable. A probe failure only reads as "needs login" when it was classified as an auth failure.

### 中文

- Trae 在请求遇到 401 时会先续期一次并在同一账号上重试；续期过程中的网络/上游故障不再把账号标成「需要登录」。
- Trae、WorkBuddy 续期成功后清除遗留的「需要登录」状态，卡片不再与实时调度状态互相矛盾。
- WorkBuddy 保活覆盖所有启用账号，不再只保开启自动签到的账号。
- 账号卡片按统一顺序判定状态：已停用、恢复中、需要登录、额度用尽、冷却中、启动中、就绪、不可用；探测失败只有被判定为鉴权失败时才显示「需要登录」。
