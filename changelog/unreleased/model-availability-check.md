### English

- Added a one-click model availability check on the Models page. It sends one minimal 1-token, thinking-disabled live request per model per available account, then reports per-account reachability and latency without changing routing cooldowns or the Usage page totals.
- The Models page status column now shows the latest availability result instead of custom/default settings, and the action column is labeled Model details.
- Removed the console reasoning-strength default selector. The Models page now shows the maximum context, the complete gateway-accepted reasoning levels for each model, and image input capability.

### 中文

- 模型页新增一键可通性检查：对“每个模型 × 每个可用账号”发送一次仅 1 token、关闭思考的真实请求，返回逐账号可通状态与延迟，且不会改变路由冷却状态或用量页统计。
- 模型页状态列改为显示最近一次可通性检查结果，不再显示默认/自定义；操作表头改为“模型详情”。
- 移除控制台推理强度默认选择器；模型页改为展示最大上下文、每个模型通过该网关可使用的完整思考档位和图片输入能力。
