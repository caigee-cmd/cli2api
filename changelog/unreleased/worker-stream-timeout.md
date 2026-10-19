### English

- Qoder streams with no first byte in 30s now fail over to the next account instead of hanging (`QODER_STREAM_TTFB_MS`).
- Worker aborts a stream only after 300s with no new chunk (`QODER_STREAM_TOTAL_MS`); long active streams are not cut.
- Unresponsive Qoder workers are detected and restarted automatically.

### 中文

- Qoder 流式请求 30 秒内无首字节会切换到下一个账号，不再挂起（`QODER_STREAM_TTFB_MS`）。
- worker 仅在连续 300 秒无新数据时中止流（`QODER_STREAM_TOTAL_MS`），持续输出的长流不会被截断。
- 无响应的 Qoder worker 会被自动发现并重启。
