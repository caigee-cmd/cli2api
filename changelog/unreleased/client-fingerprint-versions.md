### English

- Qoder requests now use one shared client version (1.1.32) for `cosy-version`, the signed payload and the User-Agent, instead of the stale 0.1.43 on auth/status calls.
- Qoder requests no longer send the Go default User-Agent; they send `QoderWork/<version>` for the China edition and `Qoder/<version>` for the international edition.
- Qoder inference streams now include `cosy-machineos` and a per-account stable `cosy-machinehostname`, matching the official client.
- WorkBuddy client versions are updated: CLI 2.161.0, desktop 5.8.4 (international) / 5.8.3 (China), IDE 2.168.2; desktop User-Agents use the three-part form, e.g. `WorkBuddy/5.8.4 workbuddy-ai/5.8.4 CLI/2.161.0`.

### 中文

- Qoder 请求的 `cosy-version`、签名载荷和 User-Agent 现统一使用同一个客户端版本（1.1.32），不再在认证/状态接口上使用过时的 0.1.43。
- Qoder 请求不再发送 Go 默认 User-Agent，国内版使用 `QoderWork/<版本>`，国际版使用 `Qoder/<版本>`。
- Qoder 推理流式请求新增 `cosy-machineos` 和按账号稳定派生的 `cosy-machinehostname`，与官方客户端一致。
- WorkBuddy 客户端版本更新：CLI 2.161.0、桌面端 5.8.4（国际版）/ 5.8.3（国内版）、IDE 2.168.2；桌面端 User-Agent 改为三段式，例如 `WorkBuddy/5.8.4 workbuddy-ai/5.8.4 CLI/2.161.0`。
