### English

- 1. Codex outbound cloaking User-Agent bumps to official stable `0.166.0` (`rust-v0.166.0`); CPA pin stays at 0.154.
- 2. Login page left panel plays the live in-flight demo (slower than Overview) instead of the static title; Access chat empty state, assistant provider marks, and copy control align with the rest of the console.
- 3. Experimental Huawei Cloud CodeArts Agent (码道) provider: OAuth PKCE, OpenAI-compatible chat/SSE, catalog discovery (drops unusable models), vision and reasoning from the catalog, quota meters, and daily/new-user credit check-in. Single-account E2E and two-account failover/session affinity are done; quota-exhaustion failover remains pending.

### 中文

- 1. Codex 出站伪装 User-Agent 升至官方稳定版 `0.166.0`（`rust-v0.166.0`）；CPA pin 仍为 0.154。
- 2. 登录页左侧改为实时进行中演示（节奏慢于 Overview），不再是静态标题；访问页聊天空状态、助手供应商标与复制控件与控制台其他位置一致。
- 3. 新增实验性华为云码道 CodeArts 供应商：OAuth PKCE、OpenAI 兼容对话/SSE、模型目录（剔除不可用模型）、按目录声明的视觉与 reasoning、额度计量，以及每日/新用户积分签到。单账号端到端与双账号失败切换/会话粘性已验收；额度耗尽切换仍待验收。
