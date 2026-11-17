### English

- Trae plan-locked models are no longer advertised through accounts that cannot use them. When an account reports a `1005` plan limit, that model is removed from its routing catalog, kept out of later catalog refreshes, and the request fails over instead of cooling every model on the account; when no account can serve it, `/v1/models` and chat report it as unavailable rather than "all accounts are cooling down".

### 中文

- Trae 未解锁的会员模型不再通过不可用的账号对外提供。账号返回 `1005` 套餐限制时，该模型会从它的路由目录中移除，并在后续目录刷新中保持剔除；请求会切换账号，而不是让整个账号所有模型进入冷却。若所有账号都无法提供该模型，`/v1/models` 和聊天接口会返回模型不可用，而不是“所有账号都在冷却”。
