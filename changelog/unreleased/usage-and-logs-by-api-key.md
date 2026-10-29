### English

- Logs and Usage pages can filter by API key: request logs record which named console API key issued each request, and `GET /api/logs/requests`, `GET /api/logs/stats`, `GET /api/logs/usage` and `GET /api/usage/cost` accept `api_key=<key id>`. Requests made with the console key stay unattributed.

### 中文

- 日志页和用量页支持按 API 密钥筛选：请求日志记录每次请求使用的是哪把具名控制台密钥，`GET /api/logs/requests`、`GET /api/logs/stats`、`GET /api/logs/usage` 和 `GET /api/usage/cost` 接受 `api_key=<密钥 id>`。使用控制台密钥的请求不计入任何具名密钥。
