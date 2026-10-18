# web/src/features/player MODIFICATIONS

用途：Miyabi 的播放单页（`/play`）。

## 文件
- `page.tsx`：播放单页。桌面：左播放器（自适应占满）、右目录卡片；顶部不再有「收起目录」按钮（收起/展开统一由分隔线把手承担）；分隔线上有把手，**点击动画收起/展开**（栅格列 `20rem↔0` + opacity 过渡），**左右拖拽调宽**（`MIN_RAIL 200 / MAX_RAIL 520`，拖到 <150 吸附收起），**双击复位**；宽度与收起状态经 `RAIL_PREF_KEY` 写入 `localStorage` 记忆（`readRailPref` 载入，`--rail-w` 驱动栅格列）。移动端（`<lg`）：目录改为贴底的可拖拽抽屉（`peek/half` 目标 `30 / 56dvh`，`full` 目标 `88dvh`；`snap` + `dragHeight` 状态，把手 pointer 拖拽 / 轻点循环档位，点选影片后回落到 half）；播放器 `section` 用 `self-start` 只占自身高度；抽屉高度由「播放器底边」实测值钳制——peek/half 保证顶边落在播放器下方（不遮挡视频底部操作条），仅 full 允许覆盖。桌面高度与栅格对齐不受影响。共通：来源 chips（本地全部 / 各本地目录 / 115 / 收藏占位）；影片级列表（封面 / 番号 / 标题；番号缩小为 `text-[0.6rem]`，标题置于番号下方一行，不再显示分段数）内部独立滚动 + 懒加载；上一段 / 下一段；连播；目录搜索（后端 `q`，250ms 防抖）；播放器控制面板可手动收起（工具栏眼睛按钮 → `setControlsHidden`，隐藏后面板 `display:none`，点击画面切换播放/暂停，`onReady`/`useEffect` 同步状态）。播放态与列表选中态解耦：`playing` state 由 `play()` 与 `onChange` 写入，切换来源 / 搜索只清空列表选中（`setCurrent(-1)`）**不再 `stop()` 播放**——只有上一段 / 下一段 / 点选影片等视频强相关操作才切换播放。底部信息条展示**刮削标题**（`PlayerTrack.name = item.title || item.code`）与番号，不再用文件名。来源 chips（启用时）与影片行加 `cursor-pointer` 手型（禁用 chip 仍 `cursor-not-allowed`）。
- `player-stage.tsx`：把 `window.MiyabiPlayer.create` 挂到容器，用 `handlers` ref 传回调（避免重建播放器），通过 `onReady` 把 `PlayerInstance` 交给页面；`onChange` 透传 `(index, source)` 供页面记录正在播放项。
- `player-assets.ts`：按需单次加载 `/player/**` 的 CSS/JS（xgplayer、hls、TmPlayerControls、GIF、内核）。

## 数据
- `GET /api/playables?scope=local|remote|favorite&directory=&q=搜索词&page=&limit=`（`internal/api/playables.go`；`q` 匹配番号或标题）。
- 播放地址：本地 `/api/local/play/:fileID`；115 `/api/strm/play/:fileID`（同源 cookie，token 不进 URL）。

## 布局约束
- 容器 `max-w-[84rem]`；`main h-dvh` 全程不整体滚动（含移动端）。
- 桌面（`lg`）：`grid-cols-[minmax(0,1fr)_var(--rail-w)]`（播放器自适应 / 目录可变宽，`--rail-w` 由 `railWidth`/`collapsed` 决定）；栅格行 `lg:grid-rows-1`，宽度过渡走 `grid-template-columns`。播放器 `section` `lg:self-stretch` 撑满整行、工具栏 `mt-auto` 贴底，使左侧上/下边与右侧目录卡片对齐（移动端仍 `self-start`）。
- 移动端（`<lg`）：栅格单行 `grid-rows-[auto]`、播放器 `self-start`（避免被撑满整屏）；目录为 `fixed` 底部抽屉，`height` 由 `viewport`（`window.innerHeight` + `playerSectionRef.getBoundingClientRect().bottom`，`ResizeObserver` 跟踪）算出，peek/half 钳制在播放器下方，full 覆盖；列表底部留 `pb-24` 避开悬浮导航。断点用 `window.matchMedia('(min-width:1024px)')`（`useIsDesktop`）区分两侧行为。

## 验证
`pnpm build` / `pnpm lint` / `pnpm test`；`node --check web/public/player/js/player-core.js`；浏览器实测由用户执行。
