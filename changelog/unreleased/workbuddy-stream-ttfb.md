### English

- WorkBuddy streaming requests now fail over when the first response byte never arrives within `WORKBUDDY_STREAM_TTFB_MS` (aligned with Qoder’s first-byte timeout), without dropping the existing 401 token-refresh path. Based on external contribution [#336](https://github.com/caigee-cmd/cli2api/pull/336) by [@YunMo-yanyu](https://github.com/YunMo-yanyu).

### 中文

- WorkBuddy 流式请求在 `WORKBUDDY_STREAM_TTFB_MS` 内收不到首字节时会切换账号（对齐 Qoder 首字节超时），并保留原有 401 刷新 token 路径。基于外部贡献 [#336](https://github.com/caigee-cmd/cli2api/pull/336)（[@YunMo-yanyu](https://github.com/YunMo-yanyu)）。
