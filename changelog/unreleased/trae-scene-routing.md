### English

- Trae chats are now routed to the catalog scene that actually serves each model. The whole catalog is served through `chat_v3` (which also carries the Max-mode tiers), and only models that scene does not list fall back to `solo_work_lite`. Previously every chat went through `solo_work_lite`, so the six models that scene does not carry (Doubao-Seed-Code, deepseek-v4.1-flash, glm-5.3-flash, glm-5.3-flashx, kimi-k2.8-preview, qwen3.8-flash) failed with a param error, and Max mode was sent to a scene without Max tiers. This is derived from catalog membership, so new models route correctly on the next refresh without changes.

### 中文

- Trae 聊天现在按「实际提供该模型的目录场景」路由。整个目录默认走 `chat_v3`（该场景也是唯一带 Max 档的），仅当 `chat_v3` 不收录某模型时才回退到 `solo_work_lite`。此前所有聊天一律走 `solo_work_lite`，导致该场景没有的 6 个模型（Doubao-Seed-Code、deepseek-v4.1-flash、glm-5.3-flash、glm-5.3-flashx、kimi-k2.8-preview、qwen3.8-flash）报参数错误，且 Max 模式被发到了没有 Max 档的场景。该路由由目录收录关系推导，新模型下次刷新即自动归位，无需改码。
