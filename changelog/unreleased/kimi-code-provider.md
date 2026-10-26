### English

- Add the China-region Kimi Code provider: sign in with the official device-code flow (or import the credential the Kimi Code client already stored), discover the subscription's real model list with its context window, thinking efforts, vision and tool support, and serve `/v1/chat/completions`, `/v1/responses` and `/v1/messages` from it.
- Kimi Code accounts show the four subscription usage windows (5-hour, 7-day, monthly, monthly-code) in the console, refresh their access token automatically, and can be routed alongside every other provider in the account pool.
- Kimi Code models accept a console reasoning default on the Providers page; a request that does not specify a level uses it, then the model's own default.
- Kimi Code is import-only: import the credential the client already stored, but export is not offered, because the credential is a bearer token pair and exporting it would hand out the account.

### 中文

- 新增中国区 Kimi Code Provider：用官方设备码授权登录（也可导入 Kimi Code 客户端已保存的凭证），自动发现该订阅真实可用的模型，以及每个模型的上下文、Thinking 档位、视觉和工具支持，并通过它提供 `/v1/chat/completions`、`/v1/responses` 和 `/v1/messages`。
- Kimi Code 账号会在控制台显示四个订阅额度窗口（5 小时、7 天、月度、月度 Code），自动刷新访问令牌，并和其他供应商一样进入账号池参与调度。
- Kimi Code 模型可在「供应商」页设置控制台默认思考强度：请求未指定档位时先用它，再用模型自身的默认档位。
- Kimi Code 只支持导入、不支持导出：可以导入客户端已保存的凭证，但控制台不提供导出，因为凭证是一对 bearer token，导出等于把账号交出去。
