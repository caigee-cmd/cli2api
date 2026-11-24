### English

- Aligned the in-process client identities with the current official builds: Qoder CLI `1.1.96` (Bun `1.4.4`, global inference on `api2.qoder.sh`), WorkBuddy desktop `5.11.3` CN / `5.11.4` Global with bundled CLI `2.176.0`, Trae CN `3.4.24` (AI extension `2.3.106768`, version code `20260212`), and QwenWork `2.1.0` build `26111906` with COSY `1.1.71`.
- WorkBuddy chat now sends the desktop `5.11.3`/`5.11.4` UA, `X-Client-*` identity and `X-Agent-Runtime: local-agent`, and drops the obsolete `X-Root-Request-ID`/`X-No-*` placeholders. Billing and check-in use the desktop AuthService shape (`axios/1.16.1`, a fresh `X-Request-ID`).
- WorkBuddy CN now uses the official primary entry `www.workbuddy.cn`; the old `copilot.tencent.com` host still resolves, but the client no longer sends there by default.
- WorkBuddy check-in now queries `/v2/billing/meter/checkin-activity-status` before claiming. Inactive campaigns and same-day repeats are reported as `skipped`/`already` without a duplicate claim. The Global edition no longer advertises or attempts daily check-in because the official desktop disables it.
- Qoder, Trae, QwenWork, MiniMax Code, CodeArts, and Doubao check-in flows were re-audited; no public endpoint change was found, so their claim paths remain as-is.

### 中文

- 将进程内客户端身份对齐到当前官方客户端：Qoder CLI `1.1.96`（Bun `1.4.4`，国际版推理走 `api2.qoder.sh`）、WorkBuddy 国内桌面端 `5.11.3` / 国际版 `5.11.4`（内置 CLI `2.176.0`）、Trae 国内版 `3.4.24`（AI 扩展 `2.3.106768`，版本码 `20260212`）、千问办公 `2.1.0`（构建 `26111906`，COSY `1.1.71`）。
- WorkBuddy 对话改用桌面端 `5.11.3` / `5.11.4` 的 UA、`X-Client-*` 身份和 `X-Agent-Runtime: local-agent`，去掉过时的 `X-Root-Request-ID` / `X-No-*` 占位头；积分与签到改用桌面 AuthService 的 `axios/1.16.1` 身份和每次新生成的 `X-Request-ID`。
- WorkBuddy 国内版改用官方主入口 `www.workbuddy.cn`；旧的 `copilot.tencent.com` 仍能解析，但客户端默认不再请求。
- WorkBuddy 签到在领取前先查询 `/v2/billing/meter/checkin-activity-status`。活动未开启和今日已签分别返回 `skipped` / `already`，不会再重复领取。由于官方国际版桌面端关闭了签到，国际版不再声明或尝试每日签到。
- 重新核对了 Qoder、Trae、千问办公、MiniMax Code、CodeArts、豆包签到流程；未发现公开端点变更，领取路径保持不变。
