# web/src/features/player MODIFICATIONS

用途：Miyabi 的播放单页（`/play`）。

## 文件
- `page.tsx`：播放单页。桌面：左播放器（自适应占满）、右目录卡片；顶部不再有「收起目录」按钮（收起/展开统一由分隔线把手承担）；分隔线上有把手，**点击动画收起/展开**（栅格列 `20rem↔0` + opacity 过渡），**左右拖拽调宽**（`MIN_RAIL 200 / MAX_RAIL 520`，拖到 <150 吸附收起），**双击复位**；宽度与收起状态经 `RAIL_PREF_KEY` 写入 `localStorage` 记忆（`readRailPref` 载入，`--rail-w` 驱动栅格列）。移动端（`<lg`）：目录改为贴底的可拖拽抽屉（`peek/half` 目标 `30 / 56dvh`，`full` 目标 `88dvh`；`snap` + `dragHeight` 状态，把手 pointer 拖拽 / 轻点循环档位，点选影片后回落到 half）；播放器 `section` 用 `self-start` 只占自身高度；抽屉高度由「播放器底边」实测值钳制——peek/half 保证顶边落在播放器下方（不遮挡视频底部操作条），仅 full 允许覆盖。桌面高度与栅格对齐不受影响。共通：来源 chips（本地全部 / 各本地目录 / 115 / 收藏占位）；影片级列表（封面 / 番号 / 标题；番号缩小为 `text-[0.6rem]`，标题置于番号下方一行，不再显示分段数）内部独立滚动 + 懒加载；上一段 / 下一段；连播；目录搜索（后端 `q`，250ms 防抖）；播放器控制面板可手动收起（工具栏眼睛按钮 → `setControlsHidden`，隐藏后面板 `display:none`，点击画面切换播放/暂停，`onReady`/`useEffect` 同步状态）。播放态与列表选中态解耦：`playing` state 由 `play()` 与 `onChange` 写入，切换来源 / 搜索只清空列表选中（`setCurrent(-1)`）**不再 `stop()` 播放**——只有上一段 / 下一段 / 点选影片等视频强相关操作才切换播放。底部信息条展示**刮削标题**（`PlayerTrack.name = item.title || item.code`）与番号，不再用文件名。来源 chips（启用时）与影片行加 `cursor-pointer` 手型（禁用 chip 仍 `cursor-not-allowed`）。
- `player-stage.tsx`：外层 `div` 只负责尺寸与圆角（`aspect-video w-full lg:h-full`，桌面填满 section，移动端保持 16:9），播放内核挂在内层 `size-full` 的 `div` 上。两者分开是因为内核会给容器加 `.miyabi-player`（带 `height:100%`），而 class 由 React 管理；以前切无边框时 React 覆盖 class 会把它冲掉，退出后播放器从填满塌成 16:9，下方空出一块。把 `window.MiyabiPlayer.create` 挂到容器，用 `handlers` ref 传回调（避免重建播放器），通过 `onReady` 把 `PlayerInstance` 交给页面；`onChange` 透传 `(index, source)` 供页面记录正在播放项；`onMetadata` 透传片源尺寸。
- `player-assets.ts`：按需单次加载 `/player/**` 的 CSS/JS（xgplayer、hls、TmPlayerControls、GIF、内核）。
- `window-mode.ts`：桌面壳的无边框窗口态。只有壳里才 invoke `set_player_window_mode`；浏览器里全部是空操作，页面自动降级。`isDesktopWindow()` 决定「无边框播放」按钮是否出现。调用时可带上片源宽高比，供壳调整窗口比例。
- `web/src/stores/shell-chrome.ts`：`navHidden` 让整屏页面（播放页）隐藏底部悬浮导航，`AppShell` 读取它。桌面壳里进播放页即隐藏，离开恢复；工具栏左侧的箭头按钮补回「返回媒体库」入口。

## 无边框播放（桌面壳）
- 三态 `windowed / borderless / fullscreen`，由 `page.tsx` 的 `windowMode` 持有。工具栏「无边框播放」按钮进入 `borderless`；`Escape` 退一层（`fullscreen → borderless → windowed`）。
- 壳侧 `set_player_window_mode`：进 `borderless` 先记下窗口尺寸与位置，再 `set_decorations(false)`；回 `windowed` 恢复装饰并还原尺寸位置；`fullscreen` 用 `set_fullscreen(true)`。
- 画面比例：`player-core.js` 在 `loadedmetadata` 时通过 `onMetadata` 上报片源真实尺寸。进无边框时把宽高比传给壳，壳按当前显示器把窗口调成同比例，首帧正好铺满，不裁不黑。之后窗口比例一旦偏离片源，`PlayerStage` 加上 `miyabi-player-cover`，把 `object-fit` 切成 `cover` 裁边填充；比例回到一致时自动退回 `contain`。
- 目录：无边框时目录不再占栅格列，改成 `fixed right-0` 的右侧抽屉，默认 `translate-x-full` 收在屏幕外；鼠标进入右侧 24px 热区滑入，指针离开抽屉自动收起。**点选影片不收起**，可以连续选片；`Escape` 先收抽屉，再退出无边框。面板材质与底部悬浮导航一致：`bg-background/85` + `backdrop-blur` + `border-border/70`，不透明底和左缘渐变都已撤掉。
- 快捷键：`←` / `→` 快退 / 快进 3 秒（`player-core.js` 的 `seekBy`，夹在 0 与 duration 之间）；焦点在输入框、文本域或下拉框时不拦截。
- 右边黑带：根因是 `globals.css` 里 `html { scrollbar-gutter: stable }` 永久保留了约 10px 滚动条槽，`fixed inset-0` 的 `main` 拿到的仍是被扣掉槽的宽度，右侧留出一条背景。无边框时给 `html` 加 `player-borderless` 类（`scrollbar-gutter: auto` + `overflow: hidden`）恢复满宽，退出时移除。实测 `main` 宽度 1270 → 1280。
- 窗口按钮不常驻：顶部 48px 是透明热区，鼠标进入才淡入右上角的还原 / 全屏 / 关闭，离开即淡出。拖动靠顶栏的 `data-tauri-drag-region`。
- 底部留白：`main` 的 `pb-24` 原本是给悬浮导航让位；桌面壳里导航已隐藏，改用 `pb-4`，避免播放器下方空出一条背景色。浏览器保留 `pb-24`。
- 页头：桌面壳里不渲染「播放」标题那块，播放器直接顶到上方；浏览器和移动端保留。
- 离开页面时兜底恢复 `windowed`，避免留下无标题栏、退不出去的窗口。
- 不复用 `tm-player-controls.css` 里的 `.video-container:fullscreen`：那批选择器的容器类名与实际 DOM（`.miyabi-player` / `.miyabi-player-stage`）不匹配，是死代码。

## 数据
- `GET /api/playables?scope=local|remote|favorite&directory=&q=搜索词&page=&limit=`（`internal/api/playables.go`；`q` 匹配番号或标题）。
- 播放地址：本地 `/api/local/play/:fileID`；115 `/api/strm/play/:fileID`（同源 cookie，token 不进 URL）。

## 布局约束
- 容器 `max-w-[84rem]`；`main h-dvh` 全程不整体滚动（含移动端）。
- 桌面（`lg`）：`grid-cols-[minmax(0,1fr)_var(--rail-w)]`（播放器自适应 / 目录可变宽，`--rail-w` 由 `railWidth`/`collapsed` 决定）；栅格行 `lg:grid-rows-1`，宽度过渡走 `grid-template-columns`。播放器 `section` `lg:self-stretch` 撑满整行、工具栏 `mt-auto` 贴底，使左侧上/下边与右侧目录卡片对齐（移动端仍 `self-start`）。
- 移动端（`<lg`）：栅格单行 `grid-rows-[auto]`、播放器 `self-start`（避免被撑满整屏）；目录为 `fixed` 底部抽屉，`height` 由 `viewport`（`window.innerHeight` + `playerSectionRef.getBoundingClientRect().bottom`，`ResizeObserver` 跟踪）算出，peek/half 钳制在播放器下方，full 覆盖；列表底部留 `pb-24` 避开悬浮导航。断点用 `window.matchMedia('(min-width:1024px)')`（`useIsDesktop`）区分两侧行为。

## 验证
`pnpm build` / `pnpm lint` / `pnpm test`；`node --check web/public/player/js/player-core.js`；浏览器实测由用户执行。
