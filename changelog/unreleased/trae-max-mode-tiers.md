### English

- Trae max mode is now only offered on models that declare a real second tier. Models upstream tags for max mode without a larger window or larger ceilings no longer show a toggle that changes nothing (affects Doubao-Seed-2.1-Turbo, kimi-k2.6, kimi-k2.7-code).
- Turning Trae max mode off now restores the default-tier prompt/output ceilings immediately. The toggle only switches the context window server-side; the console derives the shown ceilings from the Max tier at render time, so the previously stuck Max values (e.g. 936k prompt / 64k output) no longer linger until a full catalog refresh.

### 中文

- Trae 的「更大上下文」开关现在只在**确有第二档**的模型上出现。上游标了 max mode 但没有更大窗口、也没有更大上限的模型，不再显示一个点了没反应的开关（涉及 Doubao-Seed-2.1-Turbo、kimi-k2.6、kimi-k2.7-code）。
- 关掉 Trae max mode 后，输入/输出上限**立即**回到默认档。开关在服务端只切上下文窗口；控制台展示层在渲染时从 Max 档取值，因此之前会残留的最大值（如 936k 输入 / 64k 输出）不再需要整表刷新才恢复。
