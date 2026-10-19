### English

- Qoder PAT accounts now run fully in-process (no per-account Node worker), including token refresh, model catalog, quota, and check-in. (#313)
- Quota headline sums plan + add-on + org packs; reasoning streams again for in-process chat.
- Upgrade: existing Qoder accounts keep the worker path until you re-login them with a PAT.

### 中文

- Qoder PAT 账号改为完全进程内运行（不再每账号起 Node worker），token 刷新、模型目录、额度、签到均在进程内完成。(#313)
- 额度总览合计套餐 + 资源包 + 组织资源包；进程内对话恢复思考流输出。
- 升级提示：老 Qoder 账号需用 PAT 重新登录才会切到进程内模式，否则继续走 worker。
