### English

- Send the catalog's gateway model key on Qoder chat. The model catalog publishes a friendly public ID (`qwen3.8-flash`) alongside a distinct gateway key (`qfmodel`). The worker resolved the public ID through the per-account catalog before sending, but the in-process COSY client filled `model_config.key` with whatever the caller asked for. On an account whose gateway routes only the mapped key, that request was never answered — the gateway held it open and the client waited out its own timeout with no error and no bytes in return. The pool now records the public-ID-to-gateway-key mapping from the catalog, and the chat request carries the mapped key. An account whose catalog reports no distinct key keeps its public spelling.

### 中文

- Qoder 聊天请求现在发送目录里的网关模型键。模型目录同时给出一个易读的公网 ID（`qwen3.8-flash`）和一个不同的网关键（`qfmodel`）。Worker 发送前会用账号目录把公网 ID 解析一遍，但进程内 COSY 客户端此前直接把调用方传入的名字填进 `model_config.key`。在只按映射键路由的账号上，这个请求不会得到任何响应——网关把它挂着，客户端一直等到自己的超时，既没有错误也没有数据。路由池现在记录目录给出的「公网 ID → 网关键」映射，聊天请求携带映射后的键。目录未给出不同键的账号保持公网 ID 原样。