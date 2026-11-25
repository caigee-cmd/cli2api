### English

- Scheduled check-in now learns each provider region's daily window start from the upstream status payload and runs one hour after it. Qoder Global (10:00) and Qoder CN (00:00) no longer share one schedule.
- A failed status probe keeps the learned hour instead of dropping back to a guess; providers that only expose a day flag (Trae, MiniMax Code) follow a documented built-in table that is rewritten on every probe, so a corrected table reaches existing accounts.
- The check-in window table carries its provenance per provider and region in the registry (`Earliest`, `WindowFromUpstream`, `WindowSource`), sourced from vendor docs and actively maintained open-source check-in projects instead of folklore.
- Windows that start at 23:00 or later clamp to 23:59 instead of wrapping to 00:xx and firing before the window.
- The Automatic check-in card now shows each provider region's earliest window, whether it is learned from upstream or built in, and the resulting automatic claim time.

### 中文

- 定时签到改为按「供应商 + 区域」从上游状态响应里学习每日签到窗口的开始时间，并在其后一小时执行。Qoder 国际版（10:00）与 Qoder CN（00:00）不再共用同一个时间。
- 上游状态探测失败时保留已学到的时间，不再退回猜测值；只返回天级标志的供应商（Trae、MiniMax Code）走有出处的内置时间表，且每次探测都会重写，表修正后存量账号也会跟上。
- 签到窗口表在注册表里按供应商/区域记录来源（`Earliest`、`WindowFromUpstream`、`WindowSource`），依据厂商官方文档和最近仍在更新的开源签到项目填写，不再是无出处的经验值。
- 23:00 之后开始的窗口钳制到 23:59，避免加一小时跨天变成 00:xx、在窗口开启前就执行。
- 「自动签到」卡片会显示每个区域的最早窗口、是上游自动获取还是内置时间表，以及推导出的自动签到时间。
