### English

- Qoder PAT in-process ChatStream now bounds the wait for a meaningful SSE data line after a 200 (reuses `QODER_STREAM_TTFB_MS`); heartbeat-only upstreams fail over with `upstream_empty` instead of hanging. (#344)
- QwenWork ChatStream applies the same meaningful-upstream deadline for consistency.

### 中文

- Qoder PAT 进程内 ChatStream 在收到 200 后等待有意义 SSE 数据行时也会受 `QODER_STREAM_TTFB_MS` 约束；仅心跳的上游会以 `upstream_empty` 失败并切换，而不再挂起。（#344）
- QwenWork ChatStream 同样加上有意义上游数据的等待上限，保持一致。
