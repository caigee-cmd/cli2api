### English

- Add QwenWork CN and Global daily check-in: account cards now expose the manual action and the off-by-default automatic switch, with per-account and provider default times shared with the existing scheduler.
- QwenWork check-in tries the desktop client's `daily-check-in` claim API first, then falls back to today's server-granted free credits: CN reads the daily wallet balance, Global reports the gateway gap (claim route 404) instead of a fake success.

### 中文

- 新增 QwenWork 国内版/国际版每日签到：账号卡片提供手动签到和默认关闭的自动签到开关，沿用现有的账号级/供应商级签到时间调度。
- QwenWork 签到先调用桌面客户端的每日签到领取接口，接口未开放时改为核对当日服务端发放的免费积分：国内版读取钱包日额度，国际版明确记录网关未开放的原因，不伪造成功。
