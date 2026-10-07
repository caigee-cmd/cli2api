### English

- WorkBuddy CN automatic check-in now reads the configured time in Asia/Shanghai instead of the container's local timezone. The supported Docker Compose install sets no `TZ`, so the time was previously treated as UTC and fired eight hours off the intended local time.
- A dropped connection or timeout during the WorkBuddy daily check-in is now reported as a transport error instead of `checkin parse: unexpected end of JSON input`.

### 中文

- WorkBuddy CN 的自动签到改为按 Asia/Shanghai 解释配置的时间，不再跟随容器本地时区。官方 Docker Compose 部署未设置 `TZ`，此前该时间被当作 UTC 处理，实际触发时刻比预期偏差八小时。
- WorkBuddy 每日签到遇到连接中断或超时时，现在会如实报告传输层错误，不再显示 `checkin parse: unexpected end of JSON input`。
