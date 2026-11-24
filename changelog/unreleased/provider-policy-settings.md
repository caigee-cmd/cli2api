### English

- Provider-level floors, proxy, system-prompt handling, aliases, enabled models, and check-in time are now stored as one policy per provider/region. The policy is the single source of truth: account reads overlay it at runtime, saving it refreshes the live pool instead of writing the same values back to every account, and new or imported accounts inherit it automatically. Existing provider groups are seeded once at startup from their current account values without rewriting account rows.

### 中文

- 积分/百分比保底、代理、系统提示词处理、别名、可用模型和签到时间改为按供应商/区域保存的统一策略。策略是唯一事实源：账号读取时在运行时叠加，保存策略只刷新池子，不再把同一组值写回每个账号；新建或导入账号会自动继承。已有供应商分组会在启动时用现有账号值一次性生成策略，同样不写账号行。
