### English

- WorkBuddy tool schemas no longer fail upstream with code 11129 when a client sends numeric or boolean constraints as strings. Unparseable values are dropped; a schema that is already valid is left unchanged. Trae applies the same correction before its parameters are sent as text.

### 中文

- WorkBuddy 的工具 schema 不再因为客户端把数字或布尔约束写成字符串而被上游以 code 11129 拒绝。无法解析的值会被去掉；本来就合法的 schema 保持不变。Trae 在把 parameters 转成文本发出之前做同样的纠正。
