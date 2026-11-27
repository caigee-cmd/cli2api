### English

- Trae and WorkBuddy now renew a token once per account instead of once per request: concurrent requests wait on the same per-account lock, and a 401 on an old token reuses the token another request already stored. Both providers rotate the refresh token, so this prevents a second exchange from invalidating the live session.
- An imported token with no known expiry is no longer renewed on every request; the 401 path renews it once when upstream actually rejects it.
- Trae joins the nightly session keepalive. WorkBuddy keepalive skips a token that a normal request already renewed within six hours, so the sweep no longer rotates a fresh token twice in one day.

### 中文

- Trae、WorkBuddy 的续期改为按账号串行：同一账号的并发请求共用一次换取，遇到 401 时若别的请求已写入新 token 就直接复用。两者都会轮换 refresh token，这样可避免第二次换取把仍在使用的会话顶掉。
- 导入的凭据在过期时间未知时不再每个请求都续期，改由上游真正返回 401 时续期一次。
- Trae 接入每晚的会话保活；WorkBuddy 保活会跳过普通请求在 6 小时内刚续过的 token，避免同一天把新 token 再轮换一次。
