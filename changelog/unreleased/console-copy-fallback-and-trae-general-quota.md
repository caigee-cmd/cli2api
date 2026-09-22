### English

- Console copy buttons now fall back to a hidden-textarea copy when the async Clipboard API is unavailable, so copying works when the console is served over plain http (a non-secure context), not just https/localhost.
- Trae account quota now counts only the General credit bucket; the Work-only bucket (parsed separately) is excluded so it is not summed into the General figure.

### 中文

- 控制台复制按钮在异步剪贴板 API 不可用时回退到隐藏文本框 + execCommand，因此在纯 http（非安全上下文）下打开控制台时复制也能生效，不再只支持 https/localhost。
- Trae 账号额度只统计「通用积分」桶；「Work 专属积分」桶（单独解析）不计入，避免混入通用额度。
