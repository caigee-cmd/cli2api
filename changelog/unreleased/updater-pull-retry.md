### English

- Console updates no longer hang on a stalled registry connection: each `docker pull` attempt is bounded to four minutes and retried up to three times, so layers already downloaded are reused instead of the whole preparation burning its 15-minute budget and failing.

### 中文

- 控制台更新不再被卡住的镜像拉取拖死：每次 `docker pull` 限时四分钟并最多重试三次，已下载的层直接复用，不会把整个准备阶段的 15 分钟预算耗光后失败。
