### English

- Trae login now uses the IDE (PKCE) authorization-code flow: the login URL carries a S256 `code_challenge`, and the pasted callback's `authCodeInfo` code is exchanged at `/trae/api/v3/oauth/ExchangeToken` with the matching verifier and a device public key (EC P-256). Accounts created before the switch keep refreshing with the OAuth client that minted their token (`refresh_client_id`).
- The Trae model catalog now also fetches a second scene (`chat_v3`) and merges it into the primary scene, so models the primary catalog hides as invisible become selectable. Duplicates prefer the entry carrying a credit rate.

### 中文

- Trae 登录改用 IDE（PKCE）授权码流程：登录链接带 S256 `code_challenge`，粘贴回调里的 `authCodeInfo` code 连同 verifier 与设备公钥（EC P-256）交到 `/trae/api/v3/oauth/ExchangeToken` 换取令牌。切换前创建的账号继续用签发其 refresh token 的 OAuth client 刷新（`refresh_client_id`）。
- Trae 模型目录额外拉取 `chat_v3` 场景并与主场景合并，使主目录里被标为 invisible 的模型变为可选；重复项优先保留带倍率的条目。
