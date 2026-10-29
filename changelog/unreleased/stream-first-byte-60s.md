### English

- The default first-byte deadline for Qoder and WorkBuddy streams is now 60s (`QODER_STREAM_TTFB_MS`, `WORKBUDDY_STREAM_TTFB_MS`; both were 30s). A slow thinking-model cold start gets longer before the request fails over to the next account. Set either variable to keep the old deadline.

### 中文

- Qoder 和 WorkBuddy 流式请求的默认首字节时限改为 60 秒（`QODER_STREAM_TTFB_MS`、`WORKBUDDY_STREAM_TTFB_MS`，原先都是 30 秒）。思考模型冷启动更慢时，请求会多等一会儿再切换到下一个账号。想保持原来的时限，设置对应的环境变量即可。
