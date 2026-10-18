# web/public/player MODIFICATIONS

用途：Miyabi 内置播放器的静态资源与播放内核（Vite 打包到 `dist/player`，后端静态服务直接提供）。

## 来源（由原始 `player/` 参考实现拷贝而来；该参考目录已移除，本目录为唯一真源）
- `js/`：`xgplayer.min.js`、`xgplayer-flv.min.js`、`hls.min.js`、`tm-player-controls.js`、`gifenc.min.js`、`gif-capture.js`
- `css/`：`xgplayer.min.css`、`font-awesome.min.css`、`tm-player-controls.css`
- `webfonts/`：Font Awesome 字体

未拷贝：`player.html`（独立页 UI，产品不需要）、`components.css`（含全局 `html/*/:root`，会与 Miyabi 样式冲突；GIF 面板样式由 `gif-capture.js` 自注入，只依赖 `--tm-*` 令牌）。

## 新增
- `js/player-core.js`：从 `player.html` 的 `playCore`/`setupPlayerEvents` 抽出的框架无关内核。
  - 工厂：`MiyabiPlayer.create(container, { sources, poster, startIndex, continueOnEnded, onChange, onEnded, onProgress })`
  - 实例：`play / pause / seek / next / prev / hasNext / hasPrev / current / setSources / setQueue / stop / setControlsHidden / isControlsHidden / toggleFullscreen / destroy`
  - `setControlsHidden(hidden)`：隐藏 / 显示 `#tmControlHost`（隐藏用 `display:none`，显示时 `controls.show({ autoHide:false })`）；隐藏态下点击画面切换播放 / 暂停。
  - 路由：flv/mkv→xgplayer，m3u8→hls.js，其余→原生 video；统一挂 `TmPlayerControls`。
  - 约定：原生 video `id=videoPlayer`，xgplayer 容器 `id=xgplayerContainer`——`gif-capture.js` 依赖这两个选择器。
  - 保留 GIF 截动图；丢弃拖拽、笔记/标签、文件目录树、player 自带历史。
  - 移除：`TmPlayerControls` 的沉浸模式（按钮 / `onToggleImmersive` / `getImmersive` / `setImmersive` / `is-immersive` 类及 `body.immersive-mode` CSS）。原实现容器加 `is-immersive`，与 CSS 的 `body.immersive-mode` 不匹配，功能一直失效；现仅保留普通与全屏两态。
  - `tm-player-controls.js` 控制面板空闲收起：原 `show({autoHide:'self'})` 分支与 `hideDelay` 无人调用（`hide()` 只被永不触发的定时器和无调用者的 `toggle()` 引用），面板实际永久常驻。现由 `_scheduleAutoHide()` 统一接线——播放中且指针不在面板上时 `show({autoHide:'self'})`，`hideDelay`(3200ms) 后收成 `controls-mini`（仅底部 3px 进度条，仍可拖拽）；暂停时或指针停在面板上时常驻。交互监听从底部窄条 host 改绑 `videoWrapper`（整个播放区），否则面板收起后无处唤出；`pointerenter`/`pointerleave` 维护 `_pointerOverHost`，避免指针在面板上时缩回。与 `setControlsHidden`（眼睛按钮，`display:none`）语义正交，两者独立。

## 验证
`node --check js/player-core.js js/tm-player-controls.js`；资源经 `pnpm build` 内联到 `dist/player/**`，由后端 `installFrontend` 提供。
