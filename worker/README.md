# qoder-auth-worker

常驻持有热 `QoderContext`，对外提供编码后的上游 chat 调用。

## 现状

Phase A 已证明：热 context 上调用 `prepareInferRequest` 可直连上游 SSE。  
本 worker 的 MVP 策略：

1. 启动时用一次受控 `qodercli` warmup 拿到 live context
2. 后续请求复用该 context，不再走完整 agent 冷启动
3. 对 `qoder-api-proxy` 暴露 localhost/内部 HTTP

> 长期目标仍是 worker 自己 init wasm + decrypt auth；当前先把 B/C 通路打通。

## Run

```bash
export QODERCLI_BIN=/path/to/qodercli
export WORKER_PORT=3020
export PROXY_API_KEY=change-me
node src/server.mjs
```
