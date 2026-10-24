### English

- Request-log retention is configurable via `CLI2API_LOG_RETENTION_DAYS` (default 7) and `CLI2API_LOG_MAX_ROWS` (default 20000); invalid values fall back so pruning cannot be disabled by typo. Based on external contribution [#336](https://github.com/caigee-cmd/cli2api/pull/336) by [@YunMo-yanyu](https://github.com/YunMo-yanyu).

### 中文

- 请求日志保留可通过 `CLI2API_LOG_RETENTION_DAYS`（默认 7 天）与 `CLI2API_LOG_MAX_ROWS`（默认 20000 行）配置；非法值回退默认，避免误配关掉清理。基于外部贡献 [#336](https://github.com/caigee-cmd/cli2api/pull/336)（[@YunMo-yanyu](https://github.com/YunMo-yanyu)）。
