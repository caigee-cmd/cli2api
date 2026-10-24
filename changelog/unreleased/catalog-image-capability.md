### English

- Model catalogs now expose the image-input capability each provider already reports (`WorkBuddy.supportsImages`, QwenWork `is_vl`, Devin and Codex modalities) as `supports_images` on `/v1/models` and `/api/models`. The field appears only when the provider declares support; a route merged across regions keeps it only when every region declares it; Qoder mirrors the worker catalog's `is_vl` when present.
- The documented image boundary now matches the upstream catalogs: WorkBuddy's DeepSeek V4 / V4.1, GLM, Kimi, and other `supportsImages` models accept images through the gateway, while Trae, Command, and Zhipu do not declare image input.

### 中文

- 模型目录新增 `supports_images`：把各 Provider 已解析到的图片能力（WorkBuddy `supportsImages`、QwenWork `is_vl`、Devin 与 Codex 的模态声明）透出到 `/v1/models` 与 `/api/models`。只有上游明确声明时才出现；多地域合并的路由要求每个地域都声明；Qoder 在 worker 目录返回 `is_vl` 时同步透出。
- 文档里的图片能力边界与上游目录对齐：WorkBuddy 的 DeepSeek V4 / V4.1、GLM、Kimi 等 `supportsImages` 模型可以通过网关识图；Trae、Command、Zhipu 未声明图片输入。
