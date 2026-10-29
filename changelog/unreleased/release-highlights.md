### English

- 1. Overview adds a collapsible「Live now」card: expand from the concurrency strip to see live account slots (ProviderMark, concurrency cells, real elapsed + shimmer), queue→slot / Done·Failed fly-outs, and amber rate-limit bands from `/api/live` (`workers` / `rate_limits` / `sticky`). Idle live shows zeros; mock demo only when live is unavailable.
- 2. Accounts batch import accepts official WorkBuddy client export packs (`exportType: WorkBuddy-accounts`, nested `{account, auth}`); `workbuddy.cn` maps to the CN region like `codebuddy.cn`.
- 3. Logs and Usage can filter by API key: request logs record which named console key issued each call, and `GET /api/logs/requests|stats|usage` plus `GET /api/usage/cost` accept `api_key=<key id>` (console-key traffic stays unattributed).
- 4. Access chat Flat polish: softer history-rail wash, stage continuous with the page, quieter sidebar selected/hover, softer user bubble and composer — Overview / modal solid-glass untouched.
- 5. Connection-level failures (dial, TLS handshake, reset) no longer cool the account or fail over: the client gets the transport error and the account stays routable (avoids single-account「all accounts are cooling down」loops).
- 6. Qoder and WorkBuddy stream first-byte default is now 60s (`QODER_STREAM_TTFB_MS` / `WORKBUDDY_STREAM_TTFB_MS`, was 30s) so thinking-model cold starts get longer before failover; set either env to keep the old deadline.

### 中文

- 1. Overview 新增可折叠「实时进行中」卡：从并发条展开后，按 `/api/live` 投影展示账号槽（ProviderMark、并发格、真实耗时与流光）、队列→槽位 / 完成·失败飞出，以及琥珀限流色带。空闲 live 显示 0；仅 live 不可用时跑 mock 演示。
- 2. 账号批量导入支持 WorkBuddy 官方客户端导出包（`exportType: WorkBuddy-accounts`、嵌套 `{account, auth}`），并将 `workbuddy.cn` 识别为国内区（与 `codebuddy.cn` 一致）。
- 3. 日志页与用量页支持按 API 密钥筛选：请求日志记录具名控制台密钥；`GET /api/logs/requests|stats|usage` 与 `GET /api/usage/cost` 接受 `api_key=<密钥 id>`（控制台密钥流量不计名）。
- 4. Access 聊天 Flat 回拨：历史轨柔和冷洗、舞台与页面连续、侧栏选中/悬停更安静、用户气泡与作曲区软化——Overview / 弹层 solid-glass 不动。
- 5. 连接层失败（拨号、TLS 握手、连接重置）不再给账号记冷却、也不再换号：客户端直接收到链路错误，账号保持可路由（避免单账号「所有账号都在冷却」死循环）。
- 6. Qoder / WorkBuddy 流式首字节默认时限改为 60 秒（`QODER_STREAM_TTFB_MS` / `WORKBUDDY_STREAM_TTFB_MS`，原 30 秒），思考模型冷启动可多等一会再切号；设对应环境变量可恢复旧时限。
