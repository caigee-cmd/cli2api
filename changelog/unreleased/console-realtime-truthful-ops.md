### English

- Check in all / refresh credits now fan out to every account in one pass: the new bulk endpoints return one result per account with the verbatim upstream error, and the console no longer batches or queues requests.
- Console state is pushed over `GET /api/events` (SSE) with replay by sequence id, so account health, cooldown, quota, check-in and bulk progress reach the page within a second; a 5s poll takes over when the stream is unavailable.
- The console shows real data only: the synthetic inflight demo board and its toggle are gone, an unmeasured P95 renders as "—", a force-ready account is badged, and each quota snapshot states how old it is.
- Check-in results carry the real upstream reward credits plus error code/HTTP status, and a provider that reports no status is shown as unknown instead of success.
- Requests carrying video, audio or file parts are refused with a real 400 `invalid_request` rather than being silently dropped while prompts are built, and Trae streams now fail over when the upstream accepts a request and then sends nothing.

### 中文

- 一键签到 / 一键获取积分改为一次全量扇出：新的批量接口按账号返回真实结果与上游原始错误，控制台不再分批或排队。
- 控制台通过 `GET /api/events`（SSE，按序号补发）推送状态，账号健康、冷却、额度、签到与批量进度在一秒内到达页面；通道不可用时自动降级为 5 秒轮询。
- 控制台只显示真实数据：合成并发演示面板及其开关已删除，未测得的 P95 显示“—”，强制就绪账号带徽标，额度快照标注了读取时间。
- 签到结果携带上游真实奖励积分与错误码 / HTTP 状态；上游未回报状态时显示为“未知”，不再当作成功。
- 携带视频、音频或文件片段的请求会收到真实的 400 `invalid_request`，不再在拼装提示词时被静默丢弃；Trae 流在上游接受请求却持续无响应时会切换账号重试。
