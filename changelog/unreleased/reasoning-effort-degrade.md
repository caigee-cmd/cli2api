### English

Reasoning effort now degrades the CC Switch way: when a client asks for a level the model does not declare, the gateway sends the nearest lower level the model does advertise and never upgrades. A request below the model's floor (for example `reasoning_effort: none` against a model that only offers low/high/max) lands on the cheapest declared level instead of the model default, so "do not think hard" no longer becomes full-strength thinking. Levels the model declares are forwarded untouched, and `max` is never downgraded.

### 中文

思考强度改为按 CC Switch 的方式降级：客户端要求模型没有声明的档位时，网关下发它总声明过的最近更低档，且永不升档。请求低于模型最低档时（例如对只提供 low/high/max 的模型传 `reasoning_effort: none`），落到最低可用档而不是模型默认档，因此「不要高强度思考」不会再变成满档思考。模型声明了的档位原样下发，`max` 永不被降级。
