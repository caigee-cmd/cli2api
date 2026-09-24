### English

- Devin: resolve `chat_model_uid` from the live model catalog instead of hardcoded suffix tables, so renamed or removed thinking variants (e.g. `swe-1-7`, `glm-5-2`) no longer emit stale upstream model IDs. "None" is never chosen as an implicit default effort.
- Devin: pass images embedded in tool results through to the upstream prompt instead of dropping them.

### 中文

- Devin：`chat_model_uid` 改为按实时模型目录解析，不再使用硬编码后缀表；上游已改名或移除的思考档变体（如 `swe-1-7`、`glm-5-2`）不会再发出过期模型 ID。默认档不会隐式选择 `none`。
- Devin：工具结果中携带的图片现在会透传给上游，不再被丢弃。
