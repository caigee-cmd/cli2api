### English

- Qoder check-in: when upstream returns no claimable campaign, the log and the account card's check-in tooltip now show the campaign count and an `actionType:claimStatus` summary instead of only "not open"; a token renewal response that omits securityOauthToken / refreshToken no longer clears the held tokens. (#323)

### 中文

- Qoder 签到：上游未返回可领取活动时，日志与账号卡片签到提示会带上活动数与 `actionType:claimStatus` 摘要，而不再只显示"签到活动未开放"；token 续期响应缺少 securityOauthToken / refreshToken 时保留原值，不再被清空。（#323）
