# MODIFICATIONS

本仓库只维护这一份改动记录，按目录分节。修改代码、样式、配置或文档前先读这里，记录规则与验证基线；修改完成后在对应小节回填（功能 → 修改内容 → 涉及文件与位置）。不再新建子目录级 `MODIFICATIONS.md`。

## 验证基线

- 前端：`pnpm --dir web exec tsc --noEmit`、`pnpm --dir web exec oxlint src`、`pnpm --dir web exec oxfmt <改动文件> --check`、`pnpm --dir web test`、`pnpm --dir web build`
- 播放器脚本：`node --check web/public/player/js/player-core.js` 等
- 桌面壳：本机未装 Rust，编译与产物由 CI（`.github/workflows/desktop-tauri.yml`）验证

---

## icon/

用途：桌面端应用图标。

- 目录只保留 `artwork-1024.png`（原始透明插画）、`icon-1024.png`（成品母图）、`miyabi.ico`；其余尺寸由母图生成，不再提交散图。
- 设计：深色圆角底板 `#1b2233`，圆角 22%，插画占画布 72%（可见图形约 57%）。不用白色底板——插画本身以白色为主，放白底会消失。
- 生成：`python scripts/build-ico.py icon/icon-1024.png icon/miyabi.ico`。
- 关键约束：**16–128 必须是 BMP(DIB) 条目，256 才能用 PNG**，否则资源管理器列表视图空白；插画按占比显式 `resize`，不能用 `thumbnail`（只缩小不放大，大尺寸会缩水）。

## src-tauri/

用途：Miyabi 桌面端外壳（Tauri v2 + Go 后端）。纯增量目录。

**架构**：Rust 壳启动 `runtime/miyabi-server.exe`，TCP 就绪后开原生窗口加载 `http://127.0.0.1:<port>`；前端由 Go `embed.go` 内嵌。

**文件**

- `tauri.conf.json`：`productName = miyabi`、`mainBinaryName = miyabi`（安装版与便携版都叫 `miyabi.exe`）、`bundle.icon = ["../icon/miyabi.ico"]`、`bundle.resources = { "runtime/": "runtime/" }`（后端作为资源装进 `runtime/`，不用 `externalBin`，否则安装器会把第二个 exe 摊在壳旁边）、`webviewInstallMode = embedBootstrapper`、`frontendDist = stub`。
- `capabilities/default.json`：`main` / `setup` 只给 `core:default`。
- `capabilities/desktop-setup.json`：本地 `setup` 窗口才能调 `desktop_defaults` / `pick_data_dir` / `complete_setup`。
- `capabilities/desktop-remote.json`：只允许 `http://127.0.0.1:*/*` 与 `http://localhost:*/*` 调 `desktop_config` / `apply_desktop` / `set_player_window_mode`，外加 `core:window` 的 close / start-dragging / toggle-maximize（无边框顶栏）。
- `src/main.rs`：Builder + 插件 + `setup` 启后端/开窗 + 退出回收。
- `src/sidecar.rs`：**唯一的上游契约适配点**——选端口、起 `runtime/miyabi-server.exe`（`MIYABI_LISTEN` / `MIYABI_DATA_DIR` / 可选 `MIYABI_ACCESS_PASSWORD`）、TCP 就绪等待、退出 kill。上游已删 `/api/health` 与 `healthcheck`，不要再改成 HTTP 探活。
- `src/window.rs`：`WebviewWindowBuilder`；主窗口最小 1024×640（低于 `lg` 会掉进移动布局）。
- `src/prefs.rs`：`desktop.json`；`runtime/` 里存在空文件 `portable` 才算便携版（安装版写 AppData，便携版写 `runtime/`，数据目录固定壳旁边 `data/`）。
- `src/commands.rs`：`desktop_defaults` / `pick_data_dir` / `complete_setup` / `desktop_config` / `apply_desktop` / `set_player_window_mode`。阻塞操作（起停后端、写配置）一律 `spawn_blocking`，文件夹选择用非阻塞 `pick_folder`。
- `src/log.rs`：`miyabi-desktop.log` 与配置同目录，记录启动、端口、后端进程、开窗、设置重启；2MB 轮转。
- `src/job.rs`：Windows Job Object（`KILL_ON_JOB_CLOSE`），壳被强杀时由系统回收后端；直接声明 kernel32 函数，不引 `windows-sys`。
- `stub/setup.html`：首次配置向导；便携版不问数据目录，安装版用系统文件夹对话框。
- `build.rs`：`AppManifest::commands` 生成 `allow-desktop-*` 权限，不声明会在构建期失败。
- `.gitignore`：忽略 `target/`、`gen/`、`runtime/`。

**已知取舍**

- 命令返回 `Result<(), String>` 时 `tauri::Error` 不能 `?`，要 `map_err`；`setup` 里的 `String` 错误要转 `std::error::Error`。
- 产物名：`miyabi-<version>-windows-x86_64-tauri-portable.zip` 与 `...-tauri-setup.exe`；上传用 `archive: false`。
- 两个变体：安装版 `com.ppxb.miyabi.desktop`；便携版由 `tauri.portable.conf.json` 覆盖为 `com.ppxb.miyabi.portable` + `productName=miyabi-portable`，两者可同时运行。CI/脚本顺序固定为「安装版 → `--no-bundle` 便携壳」，因为共用 `target/release/miyabi.exe`。
- 发布日志：`desktop-tauri.yml` 在 tag 构建时跑 `git-cliff`（`.github/cliff.toml`，`--current`），若 `docker-publish.yml` 还没建 Release 草稿，就自己用这份正文建；已存在则只上传附件，不改正文。
- 状态：M1/M2/M3/M5 已落地并通过 CI。未做：防火墙引导、自动更新、代码签名（见 `docs/desktop-tauri-plan.md` §11）。

## web/public/player/

用途：内置播放器静态资源与内核（Vite 输出到 `dist/player`，后端直接提供）。

- 来源：原始 `player/` 参考实现；未拷贝 `player.html` 与 `components.css`（后者含全局选择器，会冲突）。
- `js/player-core.js`：框架无关内核。
  - `MiyabiPlayer.create(container, { sources, poster, startIndex, continueOnEnded, volume, muted, rate, onChange, onEnded, onProgress, onMetadata, onPrefsChange, onToggleFullscreen, onFullscreenChange })`
  - 实例：`play / pause / seek / seekBy / setVolume / next / prev / hasNext / hasPrev / current / setSources / setQueue / stop / setControlsHidden / isControlsHidden / toggleFullscreen / setFullscreenState / destroy`
  - 路由：flv/mkv → xgplayer，m3u8 → hls.js，其余原生 video；统一挂 `TmPlayerControls`。
  - 约定：原生 video `id=videoPlayer`，xgplayer 容器 `id=xgplayerContainer`，GIF 面板 `#gcPanel`。
  - 按键：`Space`/`K`/`Enter` 播放暂停、`←→` ±3s、`Shift+←→` ±30s、`↑↓` 音量、`M` 静音；输入框聚焦与按钮聚焦时不抢。
  - 全屏：有 `onToggleFullscreen` 时交给页面（桌面壳走窗口模式），否则自己走元素全屏；监听 `fullscreenchange` 回写控制条图标。
  - 切源：`teardownEngine()` 前把当前帧画到 canvas 作为覆盖层，新源 `loadeddata` 后移除；跨域污染则跳过。
  - `ended` / `loadedmetadata` / `fullscreenchange` 监听按元素加标记只绑一次，避免换源时重复触发。
  - 移除沉浸模式（原实现的类名与 CSS 不匹配，一直是死代码）。
- `js/tm-player-controls.js`：控制条。全屏状态由外部 `setFullscreen(active)` 推送，不再嗅探 DOM；面板空闲收起由 `_scheduleAutoHide()` 接线。
- `js/gif-capture.js`：面板挂载到 `document.fullscreenElement` 或 `.miyabi-player-stage`，不再挂 `body`（否则全屏时不显示）。
- 验证：`node --check web/public/player/js/*.js`。

## web/src/components/

- `floating-nav.tsx`：底部悬浮导航。桌面断点图标 `size-6`、按钮 `sm:size-10`、内边距 `sm:p-2`；移动端 `size-11` / `size-5`。
- `movie/movie-card.tsx`：封面图片层用 `group-hover` / `group-focus-within` 压暗到 0.82，依赖外层容器的 `group`；组件本身不持有播放入口。

## web/src/features/player/

用途：播放单页 `/play`。

- `page.tsx`
  - 布局：桌面左播放器 + 右目录（`--rail-w` 可拖拽调宽、点击收起、双击复位，写 `localStorage`）；移动端目录为底部抽屉（`peek/half/full`）。
  - 桌面壳里不渲染页头、隐藏底部悬浮导航，工具栏补「返回媒体库」；底部留白改 `pb-4`。
  - 上一部 / 下一部按影片边界跳转，连播仍走 track 级。
  - 深链：解析 `?movie=<id>`，在目录中定位并播第一段；找不到继续翻页（上限 10 页），仍失败且已连 115 时切 remote 重试一次，最后提示。
  - 无边框：`windowed / borderless / fullscreen` 三态；进无边框给 `html` 加 `player-borderless`（关掉 `scrollbar-gutter` 的黑带）；目录改为右侧悬浮抽屉，鼠标进右缘 24px 热区滑出，点选不收起；`Escape` 先收抽屉再退模式。
  - 顶部窗口按钮：还原（窗口图标）/ 全屏（四角括弧）/ 关闭（✕），悬停才淡入。
  - 快捷键：页面只管 `F`（壳内切无边框 / 浏览器全屏）、`N`/`P` 上下部、`Esc`；播放类在内核。
- `player-stage.tsx`：外层管尺寸，内层 `size-full` 交给内核（内核会给容器加 `.miyabi-player`，避免 React 覆盖 class 导致退出无边框后塌陷）；所有回调经 `handlers` ref 取最新值。
- `prefs.ts`：`localStorage['miyabi.player.prefs']` 读写音量 / 静音 / 倍速，播放页与详情弹窗共用。
- `window-mode.ts`：壳内调 `set_player_window_mode`，浏览器空操作。
- `web/src/stores/shell-chrome.ts`：`navHidden` 控制播放页隐藏底部导航。

## web/src/features/library/

- `movie-card.tsx`：封面覆盖 `/play?movie=<id>` 链接，是 `MovieDetailTrigger` 的**兄弟节点**（锚点不能嵌在 `role="button"` 里）；`pointer-coarse:hidden` 让触屏隐藏覆盖层，点卡片仍进详情；悬停时 56px 半透明播放键浮现。
- `movie-actions.tsx`：右下「更多」保持原样。

## web/src/features/movie-detail/

- `player.tsx`：触屏（`pointer: coarse`）下「播放」跳转 `/play?movie=<id>`；桌面保留内嵌弹窗。共用 `prefs.ts`。
- `content.tsx`：把影片 id 传给 `MoviePlayer`。

## web/src/features/settings/

- `desktop-bridge.ts`：没有 `window.__TAURI__` 时不渲染桌面分区。
- `desktop-section.tsx`：桌面分区（数据目录只读、局域网开关 / 端口 / 访问密码）；保存调用 `apply_desktop` 由壳重启后端；分隔线由组件自身渲染。

## web/src/features/access-gate/

- `access-gate.tsx`：加载判断用 `status === 'pending'`；配置读取失败时错误提示与密码框、进入按钮同时显示。
- 验证：`web/tests/access-gate.test.tsx`。
