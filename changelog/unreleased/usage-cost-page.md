### English

- Extend the existing **Usage** console page with a **Cost** tab: quota consumed across providers, DeepSeek list-price equivalent cost (peak / off-peak), stacked cost-over-time chart, per-model breakdown, and the official price table. Stats tab (requests / latency / tokens / credits by model · account · provider) is unchanged. Adds `deepseek-flash` to the pricing catalog. Credits reuse the `#361` `request_usage_details` fallback so numbers match the Stats tab. Based on external contribution [#336](https://github.com/caigee-cmd/cli2api/pull/336) by [@YunMo-yanyu](https://github.com/YunMo-yanyu).

### 中文

- 在现有「用量」页增加 **费用** 选项卡：跨供应商订阅额度、DeepSeek 官方列表价折合费用（高峰/闲时）、费用趋势图、按模型明细与官方价格表。统计选项卡（按模型·账号·供应商的请求/延迟/token/额度）保持不变。价格目录补上 `deepseek-flash`。额度汇总沿用 #361 的 `request_usage_details` 回退，与统计页数字一致。基于外部贡献 [#336](https://github.com/caigee-cmd/cli2api/pull/336)（[@YunMo-yanyu](https://github.com/YunMo-yanyu)）。
