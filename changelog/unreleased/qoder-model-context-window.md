### English

- Present Qoder's context window like Trae's Max-context switch. Qoder reports a default window and a larger selectable window per model but has no upstream toggle, so the console now shows the same "default → larger + switch" control it uses for Trae instead of a raw number input. Turning it on sends the model's largest window as the request's `context_length`; turning it off clears the override so requests fall back to the model's default. The window values come from Qoder's own catalog (`default_context_window` / `available_context_windows`) rather than a hardcoded 180000, so glm-5.3-flash shows 1M instead of being capped at 180k. Qoder Global and CN.

### 中文

- Qoder 的上下文窗口改为按 Trae 的「更大上下文」开关呈现。Qoder 每个模型都会上报默认窗口和一个可选更大窗口，但上游没有开关字段，因此控制台不再用裸数字输入框，而是复用 Trae 同款的「默认档 → 更大档 + 开关」。开启时把该模型的最大窗口作为请求的 `context_length` 发出；关闭时清除覆盖值，请求回落到模型默认窗口。窗口数值来自 Qoder 自身目录（`default_context_window` / `available_context_windows`），不再是硬编码的 180000，因此 glm-5.3-flash 显示 1M 而不再被压到 180k。国际版与国内版均适用。
