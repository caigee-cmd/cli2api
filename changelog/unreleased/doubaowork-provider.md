### English

- Add a Doubao Work (豆包工作) provider (`doubaowork`) that turns a Doubao Work subscription into OpenAI-compatible models on the gateway, with per-account model catalogs, quota, and read-only check-in status.
- Add accounts by pasting a web-session Cookie from a logged-in doubao.com session; the server validates it on import. The gateway runs headless, so there is no browser or desktop-client auto-login.
- Chat encodes full history, tool definitions, and structured-output requirements into a single upstream request and streams the reply back as OpenAI deltas; tool-call replies tolerate fenced, extended, and truncated JSON.
- Doubao Work images are not supported on this route (the upload endpoint needs a client-side signature), so image parts are rejected with a clear message rather than silently dropped.
- Check-in status is read-only: it shows whether you checked in today and the streak, but never issues a claim request, because a failed claim consumes that day's reward with no way to recover.
- Risk-control/verification errors are treated as request errors so the gateway does not switch accounts or cool the account on them.

### 中文

- 新增豆包工作（Doubao Work）供应商 `doubaowork`，把豆包工作订阅变成网关上一组 OpenAI 兼容模型，支持按账号的模型目录、额度与只读签到状态。
- 通过粘贴已登录 doubao.com 网页会话的 Cookie 添加账号，服务端在导入时校验。网关跑在无头环境，没有浏览器或客户端自动登录。
- 对话会把完整历史、工具定义和结构化输出要求编码成单条上游请求，并把回复转成 OpenAI 增量流；工具调用回复容忍 Markdown 围栏、前后解释文字和截断 JSON。
- 该路径暂不支持豆包工作图片（上传入口需要前端生成的签名），图片会以明确提示拒绝，而不是被静默丢弃。
- 签到状态只读：显示今天是否已签、连续天数，但绝不发领取请求——领取失败会消耗当天额度且无法补救。
- 风控/验证码错误按请求错误处理，网关不会因此切换账号或冷却账号。
