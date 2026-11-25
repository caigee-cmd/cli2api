### English

- Fix the updater's post-restart health gate: images report the release tag (`v0.7.52`) while the console sends the bare semver (`0.7.52`), so the previous strict string comparison failed every deploy and forced a needless rollback. Versions are now compared as semver, ignoring the leading `v`.

### 中文

- 修复更新器重启后的健康校验：镜像里的 `/health` 回报的是发布 tag（`v0.7.52`），控制台送来的是不带 `v` 的版本号（`0.7.52`），原来按字符串严格比较会让每次部署都判定失败并触发一次无谓的回滚。现在按语义版本比较，忽略前缀 `v`。
