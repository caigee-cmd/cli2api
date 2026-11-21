### English

- Split console pages into per-route chunks so the first screen loads less JavaScript; rejected chat attachments now use the console toast instead of a browser alert.
- The overview resource panel now follows the backend's 30s sample cadence instead of polling every 5s, account countdowns share one timer, and runtime logs stop polling while the tab is hidden.
- Request statistics now stream the window once instead of running about ten SQL scans; the local 20k-row benchmark drops from ~227 ms to ~57 ms per call.
- The expanded live card now polls at 800ms instead of 400ms. The 60s concurrency sparkline is hand-drawn SVG, and the inflight depth chart loads only when expanded, keeping the 316 kB chart bundle out of the default console preload.
- The console shell no longer imports HeroUI: header and sidebar controls are dependency-free, the mobile drawer is native, and the update dialog loads on demand. Together with route-level lazy pages, the entry chunk is 422 kB (134.8 kB gzip) and preloads only `jsx-runtime` (12 kB) and GSAP (93 kB), down from the previous 461 + 239 + 94 + 13 kB preload group.

### 中文

- 控制台页面按路由拆包，首屏加载的 JavaScript 更少；聊天不支持的文件改用控制台提示，不再弹浏览器 alert。
- 概览资源面板改为对齐后端 30 秒采样节奏，不再每 5 秒重复请求；账号冷却倒计时共用一个定时器；运行日志在标签页隐藏时停止轮询。
- 请求统计改为单次扫描窗口并在 Go 内聚合，本机 20,000 行基准从约 227 ms/次降到约 57 ms/次。
- 展开态实时卡片轮询从 400ms 放慢到 800ms；60 秒并发曲线改为手绘 SVG，展开后才加载 inflight 深度图表，316 kB 图表包不再进入控制台默认预加载。
- 控制台外壳不再导入 HeroUI：顶栏与侧栏控件改为无依赖原生组件，移动抽屉改为原生实现，版本更新弹窗点击时才加载。配合路由级拆包，入口为 422 kB（gzip 134.8 kB），首屏仅预加载 `jsx-runtime`（12 kB）与 GSAP（93 kB），此前首屏组合为 461 + 239 + 94 + 13 kB。