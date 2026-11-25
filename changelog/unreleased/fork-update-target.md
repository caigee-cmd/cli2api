### English

- Releases now come from this fork by default: the console checks `guyu233/cli2api`, the host updater pulls `ghcr.io/guyu233/cli2api`, and both installers use the same repository unless `UPDATE_GITHUB_REPO`, `CLI2API_IMAGE_REPOSITORY`, or `CLI2API_GITHUB_REPOSITORY` say otherwise.
- The System update panel now prints which release repository it follows, and when the host updater is missing it says how to enable one instead of reporting a generic "temporarily unavailable" message. A failed prepare also names the missing updater daemon and the installer script.
- `release.yml` also derives the previous version from reachable `v*` tags when the repository has no published release yet, so a fork's first release continues its version line instead of starting at v0.1.0.

### 中文

- 发布源默认改为本 fork：控制台检查 `guyu233/cli2api`，宿主机 updater 拉取 `ghcr.io/guyu233/cli2api`，两个安装脚本也默认使用同一仓库；需要跟随其它仓库时用 `UPDATE_GITHUB_REPO`、`CLI2API_IMAGE_REPOSITORY`、`CLI2API_GITHUB_REPOSITORY` 覆盖。
- 系统更新的版本面板会显示当前跟随的发布仓库；宿主机 updater 缺失时给出启用方法，而不是笼统的"暂时无法更新"；准备阶段失败时错误信息也会指出缺失的 updater 守护进程和安装脚本。
- `release.yml` 在仓库还没有已发布 Release 时会从可达的 `v*` tag 推算上一个版本，fork 的首次发布会延续版本线，而不是从 v0.1.0 重新开始。
