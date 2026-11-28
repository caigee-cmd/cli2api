### English

- WorkBuddy refresh now checks the stored token with the billing API. A revoked session shows as needing re-login instead of staying ready while check-in fails.
- A billing outage during that check does not mark the account as logged out.

### 中文

- WorkBuddy 刷新会用账单接口校验已保存的 token。被吊销的会话会显示为需要重新登录，而不会在签到失败时仍显示就绪。
- 这次校验遇到账单接口故障时，不会把账号标成未登录。
