### English

- A connection-level failure (dial, TLS handshake, reset) no longer puts the account into a cooldown, and no longer fails over. The request never reached the upstream, so the account stays routable and the client gets the transport error back. Previously one handshake timeout parked a healthy account for 15s, which turned a single-account route into `all accounts are cooling down` for every request that followed.

### 中文

- 连接层失败（拨号、TLS 握手、连接重置）不再给账号记冷却，也不再换号。请求根本没到上游，账号保持可路由，客户端直接收到链路错误。此前一次握手超时会把健康账号锁 15 秒，单账号路由上随后每条请求都会变成「所有账号都在冷却」。