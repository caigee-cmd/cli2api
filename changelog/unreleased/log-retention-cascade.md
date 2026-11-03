### English

- Request-log retention no longer leaves orphan rows in `request_attempts`, `request_usage_details`, and `request_stream_diagnostics`: the purge deletes child rows in the same transaction instead of relying on `ON DELETE CASCADE`, which was inert on any connection opened without `PRAGMA foreign_keys`.
- `PRAGMA foreign_keys` now travels in the SQLite DSN, so every connection the pool opens enforces the declared cascades instead of only the one that ran the startup `Exec`.
- The Usage **Cost** tab window follows `CLI2API_LOG_RETENTION_DAYS` instead of a hard-coded 7 days, so raising retention makes the older cost history readable.

### 中文

- 请求日志清理不再在 `request_attempts`、`request_usage_details`、`request_stream_diagnostics` 里留下孤儿行：清理改为在同一事务里显式删除子表行，不再依赖 `ON DELETE CASCADE`——该级联在任何未设置 `PRAGMA foreign_keys` 的连接上都不生效。
- `PRAGMA foreign_keys` 改为写进 SQLite DSN，连接池新开的每条连接都遵守已声明的级联，而不只是启动时执行过 `Exec` 的那一条。
- 用量页「费用」选项卡的时间范围跟随 `CLI2API_LOG_RETENTION_DAYS`，不再写死 7 天；调大保留期后能看到更早的费用历史。
