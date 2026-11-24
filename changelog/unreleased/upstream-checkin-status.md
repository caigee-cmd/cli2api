### English

- Check-in status is now read from the provider instead of trusting the last local attempt. The account API returns upstream `already`, `pending`, `skipped`, or `unknown`, with the last sync time.
- One-click check-in now runs up to four accounts in parallel instead of waiting through the scheduled check-in gap, so a full account set no longer stalls for tens of seconds.
- WorkBuddy queries `/v2/billing/meter/checkin-activity-status` before claiming. Inactive campaigns and same-day repeats are reported as `skipped`/`already` without a duplicate claim. The Global edition no longer advertises or attempts daily check-in because the official desktop disables it.
- Scheduled check-in is automatic by default: the upstream daily window start plus one hour (Qoder 10:00 → 11:00, other providers 00:00 → 01:00). Manual provider times still override auto mode.
- Check-in scheduling uses the provider server clock (`Asia/Shanghai`), not the operator PC's local timezone.
- A campaign status that carries no per-day flag (CodeArts) is treated as provisional until 02:00 Asia/Shanghai; an "already claimed" replay during the first two hours of the day is not recorded as today's success, and the scheduler retries at 02:05, 12:05, and 21:00.
- CodeArts reports `already` only for the `USER_LOGIN` daily campaign, not for a used-up new-user or student reward, and posts the IDE's confirmation step when the claim returns a benefit id.

### 中文

- 签到状态改为读取上游真实状态，不再只信任本地上一次执行结果。账号接口返回上游的已签到、未签到、未开放或状态未知，并标注最近同步时间。
- 一键签到改为最多四个账号并行执行，不再逐个等待定时签到间隔，整批账号不会再卡几十秒。
- WorkBuddy 在领取前先查询 `/v2/billing/meter/checkin-activity-status`。活动未开启和今日已签分别返回 `skipped` / `already`，不会重复领取。由于官方国际版桌面端关闭了签到，国际版不再声明或尝试每日签到。
- 自动签到时间默认改为“上游每日窗口开始 + 1 小时”（Qoder 10:00 → 11:00，其他供应商 00:00 → 01:00）。供应商设置切换为手动时仍按手动时间执行。
- 签到排期按供应商服务器时钟（`Asia/Shanghai`）计算，不再跟随电脑本地时区。
- 不带“当天”字段的活动状态（CodeArts）在服务器时间 02:00 前仍视为待确认；前两小时的“已领取”回放不会被记成今天的成功，调度会在 02:05、12:05、21:00 重试。
- CodeArts 只有 `USER_LOGIN` 每日活动才显示“已签到”，不会再把已用完的新用户/学生奖励当成今天已签；领取返回 benefit id 时补发 IDE 的确认请求。
