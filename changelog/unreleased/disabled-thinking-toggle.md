### English

- Honor the OpenAI-compatible `thinking:{"type":"disabled"}` off switch on WorkBuddy and Qoder chat requests, so extraction calls no longer emit reasoning tokens on WorkBuddy.
- Reasoning effort levels remain catalog-driven; disabling thinking is a separate switch and does not change level clamping.
- Trae is intentionally unchanged: its upstream catalog advertises no `none` reasoning option for reasoning models, and every known disable signal still produces reasoning tokens.

### 中文

- WorkBuddy 与 Qoder 的聊天请求现在支持 OpenAI 兼容的 `thinking:{"type":"disabled"}` 关闭思考开关，WorkBuddy 抽取类请求不再产生思考内容。
- 推理强度仍由模型 catalog 决定；关闭思考是独立开关，不改变强度档位的钳制规则。
- Trae 有意不改动：其上游 catalog 对推理模型不提供 `none` 档，实测所有已知关闭信号仍会产生思考内容。
