### English

- Overview now shows a credit-expiry distribution: credits grouped by the calendar day they lapse, one colour per account, plus a soonest-expiry table (numeric columns centred) with the daily burn rate needed to spend each batch before it expires (FEFO). Already-expired packages that still hold remain appear in a dedicated bucket.
- Overview adds a usage breakdown table with token split (prompt / completion / total), prefix-cache hit rate, error count and latency, switchable between account / model / realm dimensions and sortable by total tokens, requests, errors or latency, following the selected window (1h / 24h / 7d / 30d / custom). The 30s / visibility refresh quietly reloads this panel with the traffic chart.
- Overview adds a credit deduction history that sums upstream `usage.credit` per account and per model, with KPI tiles for credits deducted, matched tokens, average credits per 1M tokens, valid samples and cache hit rate; requests without a credit reading are excluded and nothing is back-filled, model rates come from the regional price catalog. Credit SQL shares one nullable expression with the #361 zero-coalesced form.

### 中文

- 概览新增「积分到期分布」：按自然日聚合各账号即将失效的积分，颜色区分账号；并附最近到期提醒表（数字列居中），给出该日到期积分与到期前需要的日均消耗（FEFO）。过期但仍有余额的包单独成桶。
- 概览新增「用量明细」：按维度展示 token 构成（Prompt / Completion / 合计）、前缀缓存命中率、失败数与延迟，可在账号 / 模型 / 域之间切换，并支持按合计 Token、请求数、失败数、平均延迟排序，跟随上方所选时间窗口（1 小时 / 24 小时 / 7 天 / 30 天 / 自定义）。30 秒 / 切回可见时与流量图一并 quiet 刷新。
- 概览新增「积分扣除历史」：按账号与模型汇总上游 `usage.credit`，顶部 KPI 展示扣除积分、匹配 Token、平均积分 / 1M Token、有效样本与缓存命中率；未观测到积分的请求不计入、不做任何回填，模型倍率取自区域价格目录。积分 SQL 与 #361 收敛为可空 / 补零两种包装。
