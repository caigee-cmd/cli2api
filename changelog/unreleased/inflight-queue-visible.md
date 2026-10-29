### English

- Overview InflightLiveCard selecting (选号) wait rail is visible again: expanded `/api/live` poll speeds up to 400ms, selecting display dwell is 1.6s (Acquire still immediate), and bound-within-dwell queue items keep `account_id` so pills can fly into the account slot.
- Live queue→slot fly-in no longer depends on matching mock job ids — leaving the wait rail targets the bind account (or a single newly busy slot).

### 中文

- Overview「实时进行中」选号/排队重新可见：面板展开时 `/api/live` 轮询加快到 400ms，选号展示保留约 1.6s（Acquire 仍不拖延），dwell 内已 Bind 的队列项保留 `account_id`，便于飞入账号槽位。
- 排队→槽位飞入不再依赖 mock 同 id；离开等待轨时按 Bind 账号（或唯一新忙槽）落点。
