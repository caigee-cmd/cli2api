### English

- Update checks and the host updater's self-update no longer hardcode the upstream repository: `UPDATE_GITHUB_REPO` (service) and `--github-repo` (updater) accept any owner/repo, so a fork that publishes its own releases is detected and can refresh its updater binary.
- `deploy/install-updater.sh` and `deploy/install-updater.ps1` honor `CLI2API_IMAGE_REPOSITORY` / `CLI2API_GITHUB_REPOSITORY` and pass the release repository to the updater, so a fork deployment installs without editing the scripts.

### 中文

- 更新检查与宿主 updater 的自我更新不再写死上游仓库：`UPDATE_GITHUB_REPO`（服务端）和 `--github-repo`（updater）可指向任意 owner/repo，发布在 fork 的版本因此能被发现，updater 二进制也能随之更新。
- `deploy/install-updater.sh` 与 `deploy/install-updater.ps1` 支持 `CLI2API_IMAGE_REPOSITORY` / `CLI2API_GITHUB_REPOSITORY` 覆盖并把发布仓库传给 updater，fork 部署无需改脚本即可安装。
