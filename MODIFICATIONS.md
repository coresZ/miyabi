# MODIFICATIONS

本仓库只维护这一份改动记录，按目录分节。修改代码、样式、配置或文档前先读这里，记录规则与验证基线；修改完成后在对应小节回填（功能 → 修改内容 → 涉及文件与位置）。不再新建子目录级 `MODIFICATIONS.md`。

## 验证基线

- 前端：`pnpm --dir web exec tsc --noEmit`、`pnpm --dir web exec oxlint src`、`pnpm --dir web exec oxfmt <改动文件> --check`、`pnpm --dir web test`、`pnpm --dir web build`
- 播放器脚本：`node --check web/public/player/js/player-core.js` 等
- 桌面壳：本地调试用 `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/run-desktop.ps1`（自动解析 cargo / go / pnpm 与 `cargo metadata` 的 target 目录）；release 产物仍由 CI（`.github/workflows/desktop-tauri.yml`）验证

---

## icon/

用途：桌面端应用图标。

- 目录只保留 `artwork-1024.png`（原始透明插画）、`icon-1024.png`（成品母图）、`miyabi.ico`；其余尺寸由母图生成，不再提交散图。
- 设计：深色圆角底板 `#1b2233`，圆角 22%，插画占画布 72%（可见图形约 57%）。不用白色底板——插画本身以白色为主，放白底会消失。
- 生成：`python scripts/build-ico.py icon/icon-1024.png icon/miyabi.ico`。
- 关键约束：**16–128 必须是 BMP(DIB) 条目，256 才能用 PNG**，否则资源管理器列表视图空白；插画按占比显式 `resize`，不能用 `thumbnail`（只缩小不放大，大尺寸会缩水）。

## src-tauri/

用途：Miyabi 桌面端外壳（Tauri v2 + Go 后端）。纯增量目录。

**架构**：Rust 壳先开无边框 splash 窗口（`stub/splash.html`：图标 + 应用名 + 循环加载条），后台线程启动 `runtime/miyabi-server.exe`，TCP 就绪后开原生主窗口加载 `http://127.0.0.1:<port>`，主窗口首帧加载完成再关 splash；前端由 Go `embed.go` 内嵌。

**文件**

- `Cargo.lock`：已纳入版本管理，固定 Tauri/crate 版本；`scripts/run-desktop.ps1` 检测到它时使用 `--locked`，CI 的 `tauri build` 也会按锁文件解析。
- `tauri.conf.json`：`productName = miyabi`、`mainBinaryName = miyabi`（安装版与便携版都叫 `miyabi.exe`）、`bundle.icon = ["../icon/miyabi.ico"]`、`bundle.resources = { "runtime/": "runtime/" }`（后端作为资源装进 `runtime/`，不用 `externalBin`，否则安装器会把第二个 exe 摊在壳旁边）、`webviewInstallMode = embedBootstrapper`、`frontendDist = stub`。
- `capabilities/default.json`：`main` / `setup` / `splash` 只给 `core:default`。
- `capabilities/desktop-setup.json`：本地 `setup` 窗口才能调 `desktop_defaults` / `pick_data_dir` / `complete_setup`。
- `capabilities/desktop-remote.json`：只允许 `http://127.0.0.1:*/*` 与 `http://localhost:*/*` 调 `desktop_config` / `apply_desktop` / `enter_player_immersive` / `restore_normal_window` / `resize_player_immersive` / `load_ui_state` / `save_ui_state`，外加 `core:window` 的 close / start-dragging。
- `src/main.rs`：Builder + 插件 + `setup` 开 splash、`spawn_blocking` 跑启动流程（配置未完成→开 setup；否则等后端就绪→开主窗口）+ 退出回收。splash 至少停留 600ms，避免一闪而过。
- `src/sidecar.rs`：**唯一的上游契约适配点**——选端口、起 `runtime/miyabi-server.exe`（`MIYABI_LISTEN` / `MIYABI_DATA_DIR` / 可选 `MIYABI_ACCESS_PASSWORD`）、TCP 就绪等待、退出 kill。上游已删 `/api/health` 与 `healthcheck`，不要再改成 HTTP 探活。
- `src/window.rs`：`WebviewWindowBuilder`；splash 320×320、无边框、不进任务栏、固定深色底；主窗口隐藏创建，`PageLoadEvent::Finished` 后再显示并关 splash（中间不留空窗）；主窗口与 setup 都 `maximizable(false)`；setup 固定 560×640（`resizable(false)`），内容不改变窗口大小；主窗口固定：高度取逻辑工作区 80%，播放器 16:9 + 目录 320，窗口宽 = 播放器宽 + 320，`resizable(false)`。
- `src/prefs.rs`：`desktop.json`；`runtime/` 里存在空文件 `portable` 才算便携版（安装版写 AppData，便携版写 `runtime/`，数据目录固定壳旁边 `data/`）。同目录的 `ui-state.json` 存设备 UI 偏好（NSFW / 主题 / 播放器音量与倍速 / GIF 设置），与端口无关。
- `src/commands.rs`：`desktop_defaults` / `pick_data_dir` / `complete_setup` / `desktop_config` / `apply_desktop` / `enter_player_immersive` / `restore_normal_window` / `resize_player_immersive` / `load_ui_state` / `save_ui_state`。阻塞操作（起停后端、写配置）一律 `spawn_blocking`，文件夹选择用非阻塞 `pick_folder`。窗口：正常窗口固定（`resizable(false)`、`maximizable(false)`，尺寸按播放页内容：逻辑工作区 80% 高度 × 16:9 播放器 + 320 目录）；`enter_player_immersive` 拍快照（inner_size / outer_position / 实际 decorated / 实际 resizable）、去标题栏、`resizable(false)`、按视频宽高比严格等比例 fit（以正常播放器区宽度为基准，工作区内居中）；`resize_player_immersive` 按视频比例缩放并锚定对角；`restore_normal_window` 按快照恢复并清空，幂等，不涉及 maximize。`load_ui_state` / `save_ui_state` 读写 `ui-state.json`（临时文件 + rename，避免半截 JSON）。
- `src/log.rs`：`miyabi-desktop.log` 与配置同目录，记录启动、端口、后端进程、开窗、设置重启；2MB 轮转。
- `src/job.rs`：Windows Job Object（`KILL_ON_JOB_CLOSE`），壳被强杀时由系统回收后端；直接声明 kernel32 函数，不引 `windows-sys`。
- `stub/setup.html`：首次配置向导；便携版不问数据目录，安装版用系统文件夹对话框。数据目录为只读单行输入框，长路径省略（悬停看完整路径），不会换行撑高窗口；setup 固定 560×640。
- `stub/splash.html` + `stub/splash-icon.png`：自包含启动页（无外部资源）；图标由 `icon/artwork-1024.png` 裁边缩放到 192px。`prefers-reduced-motion` 下停掉动画。
- `build.rs`：`AppManifest::commands` 生成 `allow-desktop-*` 权限，不声明会在构建期失败。
- `.gitignore`：忽略 `target/`、`gen/`、`permissions/`（`build.rs` 自动生成）、`runtime/`。

**已知取舍**

- 命令返回 `Result<(), String>` 时 `tauri::Error` 不能 `?`，要 `map_err`；`setup` 里的 `String` 错误要转 `std::error::Error`。
- 产物名：`miyabi-<version>-windows-x86_64-tauri-portable.zip` 与 `...-tauri-setup.exe`；上传用 `archive: false`。
- 两个变体：安装版 `com.ppxb.miyabi.desktop`；便携版由 `tauri.portable.conf.json` 覆盖为 `com.ppxb.miyabi.portable` + `productName=miyabi-portable`，两者可同时运行。CI/脚本顺序固定为「安装版 → `--no-bundle` 便携壳」，因为共用 `target/release/miyabi.exe`。
- 发布日志：`desktop-tauri.yml` 在 tag 构建时跑 `git-cliff`（`.github/cliff.toml`，`--current`），若 `docker-publish.yml` 还没建 Release 草稿，就自己用这份正文建；已存在则只上传附件，不改正文。
- 启动链路：splash 先出现 → 后台线程起后端（30s 上限）→ 主窗口隐藏创建并加载 → `PageLoadEvent::Finished` 时显示主窗口并关 splash。splash 至少 600ms；配置未完成时走 splash → `setup.html`，向导开成功才关 splash。
- 兜底：后端启动失败 / 超时，或开窗失败 → splash 保留在原生错误框后方，用户关闭错误框后 `exit(1)`（先关 splash 会因“最后一个窗口关闭”提前退出）；主窗口 10s 内没有加载完成事件会强制显示主窗口并关 splash，不留隐藏空窗。
- 主题：splash 固定深色，不跟随系统；主窗口和 splash 的 `background_color` 都是 `#09090b`，避免 WebView2 首帧白闪（`Color` 来自 `tauri::utils::config::Color`）。
- 状态：M1/M2/M3/M5 已落地并通过 CI。未做：防火墙引导、自动更新、代码签名（见 `docs/desktop-tauri-plan.md` §11）。

## scripts/

- `run-desktop.bat`：双击入口。切到仓库根后用 `-ExecutionPolicy Bypass` 调 `run-desktop.ps1`，透传参数（如 `-Force`）；失败时 `pause` 保留窗口，成功启动后随应用退出。
- `run-desktop.ps1`：Windows 本地 debug 启动，不写死用户目录或 target 路径。cargo 按 PATH → `CARGO_HOME` → `%USERPROFILE%\.cargo\bin` 查找并把所在目录补进当前进程 PATH；go 按 PATH → `GOROOT` → 默认安装目录查找；`web/dist` 缺失或传 `-Force` 时跑 `pnpm install --frozen-lockfile` + `pnpm build`。用 `cargo metadata --no-deps` 读真实 `target_directory`，把 `src-tauri/runtime/miyabi-server.exe` 复制到 `<target>/debug/runtime/`（`sidecar.rs` 按壳 exe 同级 `runtime/` 解析）；构建前把 `TEMP`/`TMP` 指到 `<target>/cl-temp`，避免受限环境里 cl.exe 的 C1083；debug 的 WebView2 数据目录固定到 `<target>/webview2-debug`（可由 `WEBVIEW2_USER_DATA_FOLDER` 覆盖），避免和安装版/便携版争用；debug runtime 放置 `portable` 标记，prefs/数据库/封面/日志落在 `<target>/debug` 下，避免 `%APPDATA%` 权限问题；`Cargo.lock` 存在时追加 `--locked`；最后 `cargo build` + `cargo run`。启动前检测 `miyabi-desktop` 进程，`CARGO_BUILD_TARGET` 非空时直接报错（只支持 host debug）。

## web/public/player/

用途：内置播放器静态资源与内核（Vite 输出到 `dist/player`，后端直接提供）。

- 来源：原始 `player/` 参考实现；未拷贝 `player.html` 与 `components.css`（后者含全局选择器，会冲突）。
- `js/player-core.js`：框架无关内核。
  - `MiyabiPlayer.create(container, { sources, poster, startIndex, continueOnEnded, volume, muted, rate, onChange, onEnded, onProgress, onMetadata, onPrefsChange, onFullscreenChange })`  - 实例：`play / pause / seek / seekBy / setVolume / next / prev / hasNext / hasPrev / current / setSources / setQueue / stop / setControlsHidden / isControlsHidden / toggleFullscreen / setFullscreenState / destroy`
  - 路由：flv/mkv → xgplayer，m3u8 → hls.js，其余原生 video；统一挂 `TmPlayerControls`。
  - 约定：原生 video `id=videoPlayer`，xgplayer 容器 `id=xgplayerContainer`，GIF 面板 `#gcPanel`。
  - 按键：`Space`/`K`/`Enter` 播放暂停、`←→` ±3s、`Shift+←→` ±30s、`↑↓` 音量、`M` 静音；输入框聚焦与按钮聚焦时不抢。
  - 全屏：内核直接对 `.miyabi-player-stage` 调 Fullscreen API；Tauri 2.6+ 在 Windows 把 WebView2 全屏元素同步为窗口全屏，退出时由系统恢复；`fullscreenchange` 回写控制条图标并加 `.fullscreen-active`。画面统一 `contain`，不再按窗口比例裁切（`cover` 逻辑已删除）。
  - 切源：`teardownEngine()` 前把当前帧画到 canvas 作为覆盖层，新源 `loadeddata` 后移除；跨域污染则跳过。
  - `ended` / `loadedmetadata` / `fullscreenchange` 监听按元素加标记只绑一次，避免换源时重复触发。
  - `stage` 同时带 `video-container` 类，作为随包控制条 CSS 的紧凑/全屏状态钩子。
- `js/tm-player-controls.js`：控制条。全屏状态由外部 `setFullscreen(active)` 推送，不再嗅探 DOM；面板空闲收起由 `_scheduleAutoHide()` 接线。倍速滑块不再写 `localStorage.playbackRate`，初始值读当前 video 的 `playbackRate`（由播放器 prefs 注入），变化经 `ratechange` 回写。
- `js/gif-capture.js`：面板挂载到 `document.fullscreenElement` 或 `.miyabi-player-stage`，不再挂 `body`（否则全屏时不显示）。GIF 设置优先经 `window.MiyabiUiState` 读写 `ui-state.json`，浏览器才回退 localStorage。
- 验证：`node --check web/public/player/js/*.js`。

## web/src/

- `main.tsx`：启动时先 `await loadUiState()` 再 `createRoot`；主题从 `ui-state.json` 注入 `ThemeProvider`，`ThemeSync` 把 next-themes 的变化写回。
- `stores/ui-state.ts`：设备 UI 偏好的唯一来源（NSFW / 主题 / 播放器 / GIF）。桌面壳经 `load_ui_state` / `save_ui_state` 读写 `ui-state.json`，保存 250ms 防抖并在 `pagehide` 立即 flush；浏览器回退 `localStorage['miyabi-ui-state']`，并一次性迁移旧键。`useUiState` 用 `useSyncExternalStore` 订阅。
- `providers`/`router` 等入口不变。

## web/src/components/

- `floating-nav.tsx`：底部悬浮导航。桌面断点图标 `size-6`、按钮 `sm:size-10`、内边距 `sm:p-2`；移动端 `size-11` / `size-5`。
- `media-image.tsx` / `movie-detail/recommendations.tsx`：NSFW 判断改从 `useUiState(state => state.nsfwMode)` 读取（原 `stores/settings.ts` 已删除）。
- `movie/movie-card.tsx`：封面图片层用 `group-hover` / `group-focus-within` 压暗到 0.82，依赖外层容器的 `group`；组件本身不持有播放入口。
- `movie/play-overlay.tsx`：透明播放按钮，列表与详情共用。`Link` 到 `/play?movie=<id>`，圆形按钮 hover/focus 淡入；`hideOnTouch` 时触屏隐藏（列表保留“点封面进详情”），详情不传时触屏也可见。

## web/src/features/player/

用途：播放单页 `/play`。

- `page.tsx`
  - 布局：桌面播放页贴窗口边界——去掉 main 的 max-width/内边距/栅格 gap；播放器占左/上/下，目录固定 320 占右，目录高度 = 窗口内容高度；信息条改为播放器顶部渐隐叠加；目录手柄只切换展开/收起；移动端保持原布局。
  - 桌面壳里不渲染页头、隐藏底部悬浮导航；返回媒体库入口移到播放器顶部叠加条。
  - 上一部 / 下一部按影片边界跳转，连播仍走 track 级。
  - 深链：解析 `?movie=<id>`，在目录中定位并播第一段；找不到继续翻页（上限 10 页），仍失败且已连 115 时切 remote 重试一次，最后提示。
  - 沉浸：`enterPlayerImmersive` 去标题栏、按视频比例严格等比例 fit；原生拖边关闭，左上/右下两个热区通过 pointer 拖拽调用 `resizePlayerImmersive`，由壳按比例算另一边并锚定对角；给 `html` 加 `player-immersive`（关掉 `scrollbar-gutter` 的黑带）；目录为右侧悬浮抽屉；`Escape` 先收抽屉再退沉浸。
  - 播放器外层 wrapper `relative flex min-h-0 flex-1 flex-col`：NSFW 遮罩包一层后仍能撑满左列（曾因 wrapper 只有 `relative` 导致播放器缩成 16:9 靠在顶部）。
  - NSFW 遮罩 `z-50`；桌面顶部返回/关闭条提到 `z-[60]`，遮罩不再挡住“返回媒体库”，gate 状态下仍能离开播放页。
  - 全屏：`F` / 控制条 / 顶栏都走 `player.toggleFullscreen()`；`fullscreenchange` 退出时一律 `restoreNormalWindow()` 回正常，不回沉浸。
  - 顶部悬浮条：退出沉浸（窗口图标）/ 全屏（四角括弧）/ 关闭（✕），悬停才淡入。
  - NSFW：`nsfwMode` 开启且本次访问未确认时，不 `setSources`、不自动播放；播放器区域显示遮罩与「播放」按钮，目录可浏览但点选只记 pending；确认后本次访问内不再拦截；缩略图用 `MediaImage` 遮挡。
  - 快捷键：页面只管 `F`（全屏，gate 时忽略）、`N`/`P` 上下部、`Esc`；播放类在内核。
- `player-stage.tsx`：外层管尺寸，内层 `size-full` 交给内核（内核会给容器加 `.miyabi-player`，避免 React 覆盖 class 导致退出沉浸后塌陷）；所有回调经 `handlers` ref 取最新值；不再转发 `onToggleFullscreen`。
- `prefs.ts`：读写 `ui-state.json` 的 `player` 切片（音量 / 静音 / 倍速），播放页共用。
- `player-window.ts`：`isDesktopWindow` / `enterPlayerImmersive` / `restoreNormalWindow` / `resizePlayerImmersive`，浏览器下均为空操作。
- `web/src/stores/shell-chrome.ts`：`navHidden` 控制播放页隐藏底部导航。

## web/src/features/library/

- `movie-card.tsx`：封面覆盖 `PlayOverlay`（`/play?movie=<id>`），是 `MovieDetailTrigger` 的**兄弟节点**（锚点不能嵌在 `role="button"` 里）；`hideOnTouch` 让触屏隐藏覆盖层，点卡片仍进详情；悬停时 56px 半透明播放键浮现。
- `movie-actions.tsx`：右下「更多」保持原样。

## web/src/features/movie-detail/

- `hero.tsx`：封面容器 `relative group`；有本地视频时传 `playMovieId`，在封面图片上渲染 `PlayOverlay`（透明播放按钮，触屏也可见）；JavDB-only 详情不显示。
- `content.tsx`：`playMovieId={videos?.length ? movieId : undefined}`；不再渲染独立播放器。
- 已删除 `player.tsx`：详情页统一走 `/play`，只有播放页一个播放入口。

## web/src/features/settings/

- `desktop-bridge.ts`：没有 `window.__TAURI__` 时不渲染桌面分区。
- `desktop-section.tsx`：桌面分区（数据目录只读、局域网开关 / 端口 / 访问密码）；保存调用 `apply_desktop` 由壳重启后端；分隔线由组件自身渲染。
- `privacy-section.tsx`：NSFW 开关读写 `stores/ui-state.ts`，不再用 zustand persist + localStorage。

## web/src/features/access-gate/

- `access-gate.tsx`：加载判断用 `status === 'pending'`；配置读取失败时错误提示与密码框、进入按钮同时显示。
- 验证：`web/tests/access-gate.test.tsx`。

## docs/

- `desktop-tauri-plan.md`：§5.6 本地调试改为 `scripts/run-desktop.ps1`，记录其对 `web/dist`、`miyabi-server.exe`、`target/debug/runtime/` 与 cargo PATH 的处理。
- `player-window-modes.md`：窗口/沉浸/播放器全屏 + 播放入口与 NSFW 的方案与实现记录。定稿：正常窗口固定（`resizable(false)`、`maximizable(false)`，尺寸按播放页内容：逻辑工作区 80% 高度 × 16:9 播放器 + 320 目录，播放器/目录贴边，信息条叠加）；沉浸为播放器单窗、按视频比例 fit，左上/右下角自定义比例缩放；全屏只对播放器 stage 调 Fullscreen API、画面 contain；`Esc`/手动退出/全屏退出一律回正常界面并恢复进入沉浸前的窗口快照（尺寸/位置/实际 decorations/实际 resizable，不记录 maximized），不回到沉浸。右侧目录固定 320px，手柄只切换展开/收起。详情页独立播放弹窗删除，封面用透明播放按钮统一走 `/play`；播放页目录缩略图接入 `MediaImage`，视频在 NSFW 开启时先 gate、每次进入播放页确认一次，点击后才加载播放。含 `ui-state.json` 设备 UI 偏好持久化、壳的 `enter_player_immersive` / `restore_normal_window` / `resize_player_immersive` 设计、Tauri 2.6+ WebView2 全屏说明、验收清单。
