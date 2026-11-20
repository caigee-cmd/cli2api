### English

- CodeArts vision requests now inline remote image URLs as base64 data URLs before the call, so clients that send signed object-storage links (for example a Qoder or WorkBuddy custom endpoint) no longer fail with `InferHub.ModelArts.81001.400 unsupported image url`. Downloaded images are cached briefly so replayed turns are not re-fetched.

### 中文

- 码道视觉请求现在会在调用前把远程图片 URL 内联为 base64 data URL，发送签名对象存储链接的客户端（如 Qoder / WorkBuddy 自定义端点）不再报 `InferHub.ModelArts.81001.400 unsupported image url`。已下载的图片会短暂缓存，重放的历史轮次不会重复下载。
