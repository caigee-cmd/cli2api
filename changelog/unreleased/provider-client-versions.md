### English

- Pin every provider client version to the current upstream release: Trae CN `3.4.21` with build code `20261117` (both taken from the 2026-11-17 public release; the model catalog and chat gate on `X-Ide-Version-Code`, and a stale value silently cut the catalog from 21 models to 6), Kiro IDE `1.5.37`, Codex CLI `0.172.0`, Command Code CLI `1.93.3`, Kimi Code CLI `2.7.3`, Devin CLI `3000.14.30821`, and the WorkBuddy / CodeBuddy CLI segment `2.186.0` (CN desktop `5.11.3`, Global `5.11.4`).
- Check-in and quota paths were re-verified against the live upstream with those versions: Trae daily check-in and credit usage, WorkBuddy daily check-in, user-resource and billing identity, both unchanged. WorkBuddy CLI `2.186.0` returns the same 57-model CN catalog and streams chat exactly like `2.176.0`.

### 中文

- 所有供应商客户端版本号对齐上游当前版本：Trae 国内版版本号改为 `3.4.21`、构建号改为 `20261117`（都取 11-17 的公开正式版；模型目录与对话按 `X-Ide-Version-Code` 分档，旧值会把目录从 21 个模型静默砍到 6 个），Kiro IDE 改为 `1.5.37`，Codex CLI 改为 `0.172.0`，Command Code CLI 改为 `1.93.3`，Kimi Code CLI 改为 `2.7.3`，Devin CLI 改为 `3000.14.30821`，WorkBuddy / CodeBuddy CLI 段改为 `2.186.0`（CN 桌面端 `5.11.3`、Global `5.11.4`）。
- 签到与额度链路已按新版本对真上游复验：Trae 每日签到与积分用量、WorkBuddy 每日签到、user-resource 与计费身份均正常。WorkBuddy CLI `2.186.0` 与 `2.176.0` 返回同一套 57 个模型目录，流式对话表现一致。
