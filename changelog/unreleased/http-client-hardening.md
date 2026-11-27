### English

- Serve hashed UI assets with immutable caching and gzip, and mark `index.html`, console API responses and `/v1` responses `no-store`, so a deploy no longer leaves a stale bundle in the browser.
- Turn a handler panic before the response starts into a structured JSON 500, abort partial streams instead of truncating a success response, add standard security headers, and bound request-body read time to ten minutes.
- Give the console a request watchdog with wider budgets for batch jobs, a render error boundary instead of a blank page, and SSE reconnect jitter, a connect deadline, an idle-silence watchdog, and immediate retry when the browser comes back online; after a gateway restart the bus replays its ring instead of skipping events whose sequence numbers restarted.
- Make localStorage access fail-safe so blocked or unavailable browser storage cannot blank the console during provider initialization.

### 中文

- 带哈希的 UI 静态资源改为一年 immutable 缓存并启用 gzip；`index.html`、控制台 API 和 `/v1` 响应改为 `no-store`，发布后浏览器不会再抓着旧 bundle 不放。
- 响应开始前的 handler panic 改为结构化 JSON 500；已开始输出的流直接中断而不是返回截断的成功响应；补充通用安全响应头，并把请求体读取时间限制在十分钟。
- 控制台请求增加超时看门狗（批处理任务使用更宽的预算）；渲染异常显示错误边界而不是白屏；SSE 重连加入抖动、连接超时、静默断流看门狗，并在浏览器恢复联网时立即重试；网关重启后事件序号归零时改为重放环形缓冲，而不是漏掉事件。
- localStorage 访问改为容错：浏览器禁用或限制存储时，不会在 Provider 初始化阶段直接把控制台变成白屏。
