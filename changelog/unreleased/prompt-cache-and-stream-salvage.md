### English

- Report prompt-cache token usage for Trae, Qoder, and WorkBuddy, streaming and non-streaming. Thanks [@longlixiao-ong](https://github.com/longlixiao-ong) (#293).
- Streams that end with a `finish_reason` but no `[DONE]` no longer return a 502.

### 中文

- Trae、Qoder、WorkBuddy 流式与非流式均返回 Prompt 缓存 token 用量。感谢 [@longlixiao-ong](https://github.com/longlixiao-ong)（#293）。
- 上游带 `finish_reason` 结束但未发 `[DONE]` 的流不再报 502。
