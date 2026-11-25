### English

- Download update no longer depends on GitHub release assets: the host updater binary is copied out of the image that was just pulled, so a console update works on slow or unstable routes to GitHub.
- A missing or slow updater binary no longer fails the update. Preparation still succeeds once the target image is pulled, and the host binary is refreshed from the recreated container after the health check.
- `deploy/install-updater.sh` installs the updater from the running container first (no network download), and `scripts/start.sh` installs the managed updater automatically on Linux when no unit exists yet (`CLI2API_SKIP_UPDATER_INSTALL=1` opts out).
- The version-update dialog shows the current version as the latest one plus "Up to date" instead of a bare "—" when no newer release exists.

### 中文

- 「下载更新」不再依赖 GitHub Release 资源：宿主机 updater 二进制直接从刚拉取的镜像里取出，GitHub 线路慢或抖动时也能完成更新。
- updater 二进制下载失败不再让更新失败：目标镜像拉取成功即可准备完成，宿主机二进制会在重建容器并通过健康校验后从容器中刷新。
- `deploy/install-updater.sh` 优先从运行中的容器安装 updater（不走网络），`scripts/start.sh` 在 Linux 上检测到还没有 updater 单元时自动安装（设 `CLI2API_SKIP_UPDATER_INSTALL=1` 可跳过）。
- 版本更新弹窗在没有更新时把「最新版本」显示为当前版本并标注「已是最新版本」，不再是单个「—」。
