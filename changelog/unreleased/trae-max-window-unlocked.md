### English

- Trae models that declare a larger Max tier are unlocked by the catalogue itself: the adapter asks upstream for the bigger context window on every request, `/v1/models` advertises it, and the console shows a single context size instead of a per-model Max switch.

### 中文

- Trae 里声明了更大 Max 档的模型改为自动解锁：适配器每次请求都向上游要更大的上下文窗口，`/v1/models` 直接按大窗口声明，控制台只显示一个上下文数值，不再挂一个按模型的 Max 开关。
