### English

- Qoder check-in sends the activity page `Origin` / `Referer` headers alongside the desktop identity. The in-process PAT path had dropped them, which could make upstream return no daily credit campaign even when the activity was open.
- Global check-in retries the campaign list with the desktop COSY token when the OpenAPI bearer authenticates but exposes no claimable campaign.

### 中文

- Qoder 签到重新发送活动页需要的 `Origin` / `Referer` 头。进程内 PAT 路径之前漏掉这两个头，可能导致上游在活动开放时也不返回每日积分活动。
- 国际版在 OpenAPI token 能鉴权但拿不到可领活动时，会再用桌面 COSY token 重试活动列表。
