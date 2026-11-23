### English

- Split console pages into per-route chunks so the first screen loads less JavaScript; rejected chat attachments now use the console toast instead of a browser alert.
- The overview resource panel now follows the backend's 30s sample cadence instead of polling every 5s, account countdowns share one timer, and runtime logs stop polling while the tab is hidden.
- Request statistics now stream the window once instead of running about ten SQL scans; the local 20k-row benchmark drops from ~227 ms to ~57 ms per call.
- The expanded live card now polls at 800ms instead of 400ms, and the inflight depth chart loads only when expanded. The 60s concurrency sparkline stays on Recharts inside that lazy card, so the 316 kB chart bundle remains outside the default console preload.
- The console shell keeps its HeroUI primitives (Drawer, Button, Tooltip, Skeleton) per the design system; the first-screen win comes from route-level lazy pages plus an on-demand update dialog. The entry chunk is 431.4 kB (130.6 kB gzip) with a 348 kB preload group (shim 13 kB, shared HeroUI chunk 94 kB, i18n 148 kB, GSAP 93 kB), down from the previous 461 + 239 + 94 + 13 kB.

### 中文

- 控制台页面按路由拆包，首屏加载的 JavaScript 更少；聊天不支持的文件改用控制台提示，不再弹浏览器 alert。
- 概览资源面板改为对齐后端 30 秒采样节奏，不再每 5 秒重复请求；账号冷却倒计时共用一个定时器；运行日志在标签页隐藏时停止轮询。
- 请求统计改为单次扫描窗口并在 Go 内聚合，本机 20,000 行基准从约 227 ms/次降到约 57 ms/次。
- 展开态实时卡片轮询从 400ms 放慢到 800ms；inflight 深度图表只在展开时加载。60 秒并发曲线仍使用 Recharts 并位于该懒加载卡片内，316 kB 图表包依旧不进入控制台默认预加载。
- 控制台外壳按设计规范保留 HeroUI 原语（Drawer、Button、Tooltip、Skeleton）；首屏收益来自路由级拆包和按需加载的版本更新弹窗。入口为 431.4 kB（gzip 130.6 kB），预加载组 348 kB（shim 13 kB、HeroUI 共享块 94 kB、i18n 148 kB、GSAP 93 kB），此前为 461 + 239 + 94 + 13 kB。
