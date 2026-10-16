### English

- Trae browser login now completes on its own. The loopback listener only parked the authorization code and waited for a console poll to redeem it, but the console's poll window is far shorter than a real browser login (an SMS login can take minutes), so the poll expired first and the code was never exchanged. The account then reported `account not found` with no models, and the code expired unused. The callback now exchanges the code and stores the credential as soon as the browser reaches the loopback listener, so closing the browser or missing the poll window no longer costs the login. A pasted callback URL still works as a fallback and now actually redeems the parked code instead of reporting "login complete" while storing nothing; a callback URL carrying only `loginTraceID` is accepted for that path; and the single-use code is exchanged exactly once even when the callback, a poll, and a paste overlap.

### 中文

- Trae 浏览器登录现在会自行完成。此前 loopback 监听只把授权码暂存下来，等待控制台轮询去换取令牌，但控制台的轮询窗口远短于真实浏览器登录（短信登录可能耗时数分钟），轮询先超时退出，授权码因此从未被换取。账号随后报 `account not found` 且没有任何模型，授权码也就白白过期。现在浏览器一到达 loopback 监听，回调就会立即换取授权码并写入凭据，关闭浏览器或错过轮询窗口都不再导致登录失败。粘贴回调 URL 仍可作为兜底，并且现在会真正换取已暂存的授权码，而不是报告"登录完成"却什么都不保存；仅携带 `loginTraceID` 的回调 URL 也被接受用于该路径；即便回调、轮询与粘贴同时发生，单次使用的授权码也只会被换取一次。
