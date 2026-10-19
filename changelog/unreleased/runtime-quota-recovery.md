### English

- Accounts marked out of quota are re-checked automatically, with backoff from 5 minutes up to 24 hours.
- When every eligible account is out of quota, requests return `429 insufficient_quota` instead of "no accounts available".

### 中文

- 额度耗尽的账号会自动复查，间隔从 5 分钟逐步退避到 24 小时。
- 所有可用账号都耗尽额度时，返回 `429 insufficient_quota`，而非「无可用账户」。
