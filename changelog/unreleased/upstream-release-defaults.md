### English

- Release defaults follow upstream again: the console checks `caigee-cmd/cli2api`, `deploy/docker-compose.yml` pulls `ghcr.io/caigee-cmd/cli2api:latest`, and the host updater and both installers use the same repository unless `UPDATE_GITHUB_REPO`, `CLI2API_IMAGE_REPOSITORY`, or `CLI2API_GITHUB_REPOSITORY` say otherwise. A fork that publishes its own builds sets those variables instead of changing the defaults, so a standard deployment keeps pulling upstream images and following upstream releases.

### 中文

- 发布源默认值恢复为上游：控制台检查 `caigee-cmd/cli2api`，`deploy/docker-compose.yml` 拉取 `ghcr.io/caigee-cmd/cli2api:latest`，宿主机 updater 与两个安装脚本也默认使用同一仓库；需要跟随其它仓库时用 `UPDATE_GITHUB_REPO`、`CLI2API_IMAGE_REPOSITORY`、`CLI2API_GITHUB_REPOSITORY` 覆盖。自行发布镜像的 fork 应设置这些变量，而不是修改默认值，这样标准部署仍然拉取上游镜像、跟踪上游发布。
