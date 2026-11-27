### English

- A bulk quota refresh that fails partway now opens a dialog listing every failed account, with its provider, status badge and the upstream reason, and offers copying the whole list.
- The toast names the first failed account and its reason instead of dumping one raw upstream error, and account cards clean HTML error pages and internal account ids out of the displayed reason.

### 中文

- 批量刷新额度部分失败时会弹出对话框，逐个列出失败账号、所属平台、状态徽标和上游返回的原因，并支持一键复制全部。
- 提示条改为「账号名：原因」的形式，不再直接抛出一段上游原始错误；账号卡片也会清洗展示的原因里的 HTML 错误页和内部账号 ID。
