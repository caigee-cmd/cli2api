### English

- Added `least-quota-first` and `most-quota-first` routing strategies for both global account scheduling and aggregation model pools. They rank accounts by remaining quota ratio and place accounts with unknown quota last.
- Fixed model-pool strategy overrides so FEFO candidate counting matches account selection, and normalized stored aggregation strategies so legacy/case-variant values do not silently fall back to round robin.

### 中文

- 新增 `least-quota-first`（剩余额度最少优先）和 `most-quota-first`（剩余额度最多优先）路由策略，全局账号调度和模型池都可使用；按剩余额度百分比排序，额度未知的账号排在最后。
- 修复模型池策略覆盖时 FEFO 候选计数与实际选号不一致的问题，并在读取存储的模型池策略时做归一化，避免历史大小写或旧值静默退回轮询。
