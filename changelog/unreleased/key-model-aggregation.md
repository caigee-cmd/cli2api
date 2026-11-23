### English

- API keys can now enable cross-provider model aggregation. An aggregation key's `/v1/models` returns only the public model aliases its granted accounts define, folded into one provider-neutral name per alias; requests still route through the account pool and may only call those names.
- Account model aliases appear in `/v1/models` for ordinary keys too: one public name can route to a different native model per account and provider, while an existing real model id always wins over an alias.
- The key editor now scrolls its provider list so the save/cancel footer stays reachable on shorter screens.

### 中文

- API 密钥可以开启「跨供应商模型聚合」。开启后，`/v1/models` 只返回该密钥可访问账号上配置的公共模型别名，并按别名合并成一条、不显示供应商；请求仍按账号池路由，且只能调用这些聚合名称。
- 普通密钥的 `/v1/models` 也会展示账号模型别名：同一个公共模型名可以按账号和 Provider 路由到不同的原生模型；与真实模型同名的别名不会覆盖真实模型。
- 密钥编辑弹窗在屏幕较矮时改为滚动供应商列表，保存与取消按钮始终可见。
