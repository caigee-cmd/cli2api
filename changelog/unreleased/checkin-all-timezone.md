### English

- Check in all no longer fails silently when the reported server timezone is not a name Intl accepts. An invalid zone falls back to the browser zone, and a failure shows an error instead of leaving the button idle.
- The sidebar in-flight count no longer polls `/api/live` on every page. That poll stays on the overview, which is the page that renders it.

### 中文

- 一键签到不再因为上报的服务器时区不是 Intl 接受的名字而静默失败。非法时区会退回浏览器时区，失败时会显示错误，而不是按钮毫无反应。
- 侧栏的并发数不再在每个页面轮询 `/api/live`。这个轮询只留在真正渲染它的概览页。
