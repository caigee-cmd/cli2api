# Aggregation account deployments

- Add `account_id` to aggregation members so one logical model can target a
  specific account/deployment instead of fanning out over every account of a
  provider.
- Persist the field in SQLite and expose it through the console API and model
  pool editor.
- Keep legacy members without `account_id` on the existing provider/region
  pool behavior.
