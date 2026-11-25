### English

- Automatic check-in now follows one operator-set time per provider instead of a learned upstream window. The setting defaults to 09:00 for every provider, lives in the console system settings, and takes effect for any account without a per-account custom time as soon as it is saved; the stored per-account auto time is only a display mirror and no longer outranks the setting.
- Removed the upstream earliest-window learning and the per-provider/region provenance table (`earliest`, `window_from_upstream`, `window_source`); upstream status payloads now only report check-in status.

### 中文

- 自动签到改为跟随「按供应商统一设置的时间」，默认全部为 09:00，在控制台系统设置里调整；未单独设置时间的账号在保存后立刻按新时间排期，账号上存的自动时间只做展示、不再压过设置。
- 移除了按上游最早窗口学习以及注册表里的窗口出处表（`earliest`、`window_from_upstream`、`window_source`），上游状态只用来回报签到状态。
