### English

- A disabled account's refresh button now re-reads its live status and quota instead of failing with `account is not running`, and refreshing never puts the account back into request routing.
- A refresh that fails now shows an error toast with the reason; the account card and "Refresh credits" no longer report success for a failed refresh.

### 中文

- 停用账号的刷新按钮现在会重新读取实时状态和额度，不再报 `account is not running`，并且刷新不会把账号重新放进请求调度。
- 刷新失败时会弹出带原因的错误提示；账号卡片和「刷新积分」不会再在刷新失败时显示成功。
