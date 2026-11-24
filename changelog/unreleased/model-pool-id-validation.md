### English

- Creating a model pool now validates the model ID inside the dialog (lowercase letters, digits, `-` and `/`) and shows the reason inline, instead of submitting a request the server is bound to reject.
- Form errors in the model-pool editor stay pinned above the footer, so a validation failure is visible even when the member list is scrolled to the bottom.

### 中文

- 新建模型池时先在弹窗内校验模型 ID（只能是小写字母、数字、`-` 和 `/`），不合法就当场说明原因，不再提交一个必定被服务端拒绝的请求。
- 模型池编辑器的表单错误固定显示在底部按钮上方，成员列表滚到底也能看到校验失败的原因。
