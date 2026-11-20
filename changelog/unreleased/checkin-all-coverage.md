### English

- One-click check-in now runs on the server across every eligible account, regardless of the current browser filter/page or per-account timezone parsing. It honors the system setting for disabled accounts, reports `success`, `already`, `skipped`, and `error` separately, and no longer treats upstream `skipped` as a failure.
- Scheduled check-in now retries `skipped` attempts in the existing 21:00 retry window instead of treating them as completed for the whole day.

### 中文

- 一键签到改为服务端遍历所有符合条件的账号执行，不再受浏览器当前筛选、分页或本地时区判断影响。它会遵循“签到包含停用账号”的系统设置，并分别统计 `success`、`already`、`skipped`、`error`，不再把上游返回的 `skipped` 误判为失败。
- 自动签到不再把 `skipped` 当成当天已完成；这类尝试会在现有的 21:00 重试窗口中再试一次。
