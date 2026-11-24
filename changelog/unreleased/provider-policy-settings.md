### English

- Account-level floors, proxy, system-prompt handling, aliases, enabled models, and check-in time move to a provider-wide settings modal on the Models page. The provider policy is now the single source of truth: accounts no longer store the effective provider settings, reads overlay the policy at runtime, and saving it refreshes the live pool instead of writing the same values back to every account. Existing provider groups are seeded once at startup from their current account values without rewriting account rows. Once a provider policy exists, new or imported accounts inherit it automatically, and the account editor keeps only name, concurrency, and priority.

### 中文

- 积分/百分比保底、代理、系统提示词处理、别名、可用模型和签到时间改为模型页每个供应商分组里的统一设置。供应商策略现在是唯一事实源：账号行不再承载生效的供应商设置，运行时读取时叠加策略，保存策略只刷新池子，不再把同一组值写回每个账号。已有供应商分组会在启动时用现有账号值一次性生成策略，同样不写账号行。供应商策略存在后，新建或导入账号会自动继承，账号编辑只保留名称、并发和优先级。
