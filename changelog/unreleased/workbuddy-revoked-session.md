### English

- WorkBuddy refresh now checks the stored token with the billing API. A revoked session shows as needing re-login instead of staying ready while check-in fails.
- A billing outage during that check does not mark the account as logged out.
- A billing check that does not finish leaves the account status unchanged, so a timeout cannot turn a revoked session back into ready.

### 中文

- WorkBuddy 刷新会用账单接口校验已保存的 token。被吊销的会话会显示为需要重新登录，而不会在签到失败时仍显示就绪。
- 这次校验遇到账单接口故障时，不会把账号标成未登录。
- 账单校验没有完成时，账号状态保持原样，因此超时不会把已吊销的会话改回就绪。
