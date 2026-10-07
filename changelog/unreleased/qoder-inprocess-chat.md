### English

- Qoder chat now signs requests in the Go process and calls the gateway directly. Per-account Node workers stay for login, model catalog, and quota, and are no longer on the chat path.

### 中文

- Qoder 聊天改为在 Go 进程内签名并直连网关。每个账号的 Node worker 仍负责登录、模型目录和配额，不再参与聊天请求。
