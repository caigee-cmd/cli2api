### English

- Keep Qoder requests on their canonical model IDs instead of internal CLI enum keys so upstream chat endpoints do not stall.
- Wire proxy configuration into in-process Qoder chat so account and system proxy URLs are honored.
- Sync worker readiness into the pool when maintenance watchdog probes succeed, and wait briefly at boot for worker daemons to listen.

### 中文

- Qoder 对话请求保持公共模型名直传，不再重写为内部 CLI 枚举 key，避免上游网关超时挂起。
- 为 Qoder 进程内直连注入代理配置，支持读取账号级与系统级代理设置。
- 保活巡检探测成功时自动同步 worker 就绪状态到路由池，并增加开机启动等待以防过早探测导致节点未就绪。
