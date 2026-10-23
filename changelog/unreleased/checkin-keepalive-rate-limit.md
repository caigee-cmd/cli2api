### English

- Scheduled check-in and WorkBuddy keepalive now rate-limit across accounts: each 30s maintenance tick handles at most 2 due accounts, with ~2s + small FNV jitter between them, so a large AutoCheckin fleet no longer bursts upstream in one tick after the configured time (or after 22:00 for keepalive).

### 中文

- 定时签到与 WorkBuddy Keepalive 现按跨账号限速：每个 30 秒 maintenance tick 最多处理 2 个到期账号，账号之间约 2 秒并加小幅 FNV jitter，避免大量 AutoCheckin 账号在配置时刻（或 Keepalive 的 22:00）后同一 tick 背靠背打上游。
