### English

- Fix: a client disconnect while the gateway is still waiting for the first upstream event is reported as a cancellation. It no longer counts as a successful response or clears the account's cooldown. An upstream 200 whose stream closes before the first event fails over to another account instead of being recorded as a success.

### 中文

- 修复：网关还在等上游第一个事件时客户端断开，现在记为取消，不再算作成功响应，也不会清掉该账号已有的冷却。上游回了 200 但在第一个事件前就关闭的流会换到其他账号，不再记成一次成功。
