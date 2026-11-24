### English

- Model-pool IDs may now contain dots (`deepseek-v4.1-flash`), so an upstream model name can be reused verbatim; spaces, underscores and stray symbols are still rejected with an inline message instead of a silent server error.
- The display name is optional: leave it blank and the model ID is used, and validation errors stay pinned above the footer even when the member list is scrolled.

### 中文

- 模型池 ID 现在允许点号（`deepseek-v4.1-flash`），可以直接沿用上游模型名；空格、下划线等仍然会被就地拒绝并说明原因，不再等到服务端返回一个看不见的错误。
- 显示名称变为可选：留空则直接使用模型 ID；校验失败的错误固定显示在底部按钮上方，成员列表滚到底也可见。
