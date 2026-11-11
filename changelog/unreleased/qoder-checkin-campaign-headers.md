### English

- Fix Qoder daily check-in stopping after the first claim. Campaign requests no longer send the derived machine-identity headers that hide the daily credit campaign, an empty campaign list is retried, and a same-person already-claimed response counts as checked in today instead of a failure.

### 中文

- 修复 Qoder 每日签到只在第一次成功、之后不再领取。活动请求不再发送会把每日积分活动隐藏掉的派生机器身份头；活动列表为空时会重试；同一身份今天已经领过时记为今日已签到，不再当成失败。
