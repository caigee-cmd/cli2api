### English

- Align Qoder Cosy fingerprint with hub v1.7.3: CLI 1.1.75, desktop Cosy-Version 0.4.8, undici User-Agent, business/scene/clientip headers, lowercase `agree` data-policy; machineos on all COSY requests, hostname SSE-only.
- Bump Codex outbound cloaking User-Agent to official stable `0.164.0`.
- Pass through inbound `X-Codex-Turn-Metadata`, `X-Codex-Turn-State`, and `X-Codex-Window-Id` on Codex OAuth (non-SIWC) upstream requests, matching CLIProxyAPI’s narrow EnsureHeader set.

### 中文

- Qoder Cosy 指纹对齐 hub v1.7.3：CLI 1.1.75、桌面签到 Cosy-Version 0.4.8、undici User-Agent、business/scene/clientip 头、data-policy 小写 `agree`；machineos 全 COSY 请求携带，hostname 仅 SSE。
- Codex 出站伪装 User-Agent 升至官方稳定版 `0.164.0`。
- Codex OAuth（非 SIWC）上游透传入站 `X-Codex-Turn-Metadata` / `X-Codex-Turn-State` / `X-Codex-Window-Id`，范围与 CLIProxyAPI EnsureHeader 一致、不扩大。
