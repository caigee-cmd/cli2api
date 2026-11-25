### English

- Accounts page: new "Re-login" mode. Tick the account cards, press confirm, and the console walks the selected accounts one by one, opening each account's authorization page, polling the login status, and advancing automatically once an account is signed in. A cancel button stops the walkthrough at any point; each row keeps its own status and the verbatim upstream message.
- Accounts are authorized sequentially on purpose: WorkBuddy accounts often share one SSO session, and parallel flows would hand the same freshly issued token to different account records.

### 中文

- 账号页新增「重新登录」模式：勾选账号卡片后点确定，控制台会按顺序逐个走授权流程——打开该账号的登录页、轮询登录状态，登录成功后自动进入下一个；随时可点取消中止，每一行都保留自己的状态和上游原文消息。
- 故意串行授权：多个 WorkBuddy 账号常共用一个 SSO 会话，并发授权会把同一份新签发的令牌塞给不同账号。
