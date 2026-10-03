### English

- Add OrcaRouter as a first-class provider with two credential entries: paste an `sk-orca-…` API key, or connect with an OrcaRouter account through OAuth 2.0 + PKCE (loopback redirect, no client secret).
- Populate the model selector from the live OrcaRouter catalog, filtered per entry (`chat`, image/audio/video understanding, embedding, image generation, video, rerank), instead of free-text model entry.

### 中文

- 新增 OrcaRouter 一等 provider，提供两种凭据入口：粘贴 `sk-orca-…` 密钥，或用 OrcaRouter 账号通过 OAuth 2.0 + PKCE 连接（loopback 回调，无需 client secret）。
- 模型下拉由 OrcaRouter 实时目录生成，并按入口能力过滤（chat、图片/音频/视频理解、embedding、图片生成、视频、rerank），不再需要手填模型名。
