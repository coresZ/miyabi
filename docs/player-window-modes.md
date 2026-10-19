# 窗口、沉浸播放、播放器全屏与 NSFW 保护

> 状态：方案（未改代码）。
> 基线：0.4.14（2026-10-19）。
> 本轮定稿：正常窗口固定；沉浸窗口按当前视频比例 fit、不允许手动缩放；右侧目录固定 320px、只保留展开/收起；全屏 `contain`；详情页播放改成封面上的透明播放按钮；所有退出回“进入沉浸前的播放页 + 原始窗口状态”；NSFW 保护补到播放页；UI 偏好落 `ui-state.json`。

## 1. 定稿规则

1. **正常界面**：**固定窗口**（`resizable(false)`、`maximizable(false)`）；初始尺寸取当前显示器逻辑工作区的 80%，标题栏以实际 `is_decorated()` 为准。
2. **沉浸播放**：`decorations=false`，没有标题栏；窗口由程序**严格按视频宽高比等比例**调整（不单独改宽或高）；**不提供用户手动拖边缩放**。
3. **播放器全屏**：只对播放器 stage 调 Fullscreen API；画面 `contain`；窗口跟随全屏。
4. **所有退出都回正常**：
   - `Esc` 退出沉浸 → 正常；
   - 点“退出沉浸” → 正常；
   - 退出全屏（`Esc` / 控制条按钮）→ **正常，不回沉浸**；
   - 沉浸里进全屏，再退出 → 正常。
5. **恢复“进入沉浸前的播放页 + 原始窗口状态”**：尺寸、位置、实际标题栏状态（decorations）、实际可缩放状态（resizable）全部还原；页面回到普通播放页，不重新加载播放器。
6. **壳不再有 `windowed / borderless / fullscreen` 三态**，只有：
   - `enter_player_immersive(aspect?)`：拍快照（尺寸/位置/decorations/resizable）、去标题栏、按比例 fit；
   - `restore_normal_window()`：恢复实际标题栏状态/尺寸/位置/可缩放，幂等。
7. **详情页播放**：封面图片上放一个和列表一样的透明播放按钮，点击进 `/play?movie=<id>`；详情页不再保留独立的播放弹窗。
8. **NSFW 保护**：播放页的目录缩略图遮挡；视频在保护开启时先显示遮罩，用户点击后才加载/播放。

## 2. 三种界面

| 界面 | 标题栏 | 窗口 | 页面 | 退出 |
| --- | --- | --- | --- | --- |
| 正常 | 按实际状态（固定，不可拉伸） | 工作区 80% 的初始尺寸/位置 | 播放器 + 目录 | — |
| 沉浸 | **无** | 程序按当前视频比例 fit；不可手动缩放 | 播放器铺满，目录右侧抽屉，顶部悬浮条 | `Esc` / 退出按钮 / 离开页面 |
| 全屏 | 无（系统全屏） | 当前显示器 | 只有播放器 stage | `Esc` / 控制条退出全屏 |

**没有“全屏 → 沉浸”这条边。** 全屏退出永远回正常；想再沉浸就重新点“沉浸播放”。

## 3. 状态与所有权

```text
壳（Rust）
  ├─ WindowSnapshot：进入沉浸前的 inner_size / outer_position / decorated / resizable
  ├─ enter_player_immersive()：拍快照、去标题栏、禁缩放、按比例 fit
  └─ restore_normal_window()：恢复实际 decorations / resizable / 尺寸 / 位置

页面（React）
  ├─ immersive: boolean（页面意图）
  ├─ fullscreen: boolean（来自内核 fullscreenchange）
  ├─ nsfwRevealed: boolean（播放页本次访问是否已确认）
  ├─ 退出沉浸/全屏 → restore_normal_window()
  └─ 页面挂载/卸载/刷新 → 也调 restore_normal_window()

播放器（内核）
  ├─ stage.requestFullscreen() / document.exitFullscreen()
  ├─ fullscreenchange → 更新控制条图标 + 通知页面
  └─ 全屏元素包含视频、控制条、GIF 面板
```

原则：

- **壳只负责窗口**，不关心当前是播放页还是别的页。
- **页面只发意图**；窗口实际状态以壳为准。
- **全屏不经过壳**；Tauri 2.6+ 会把 WebView2 的全屏元素同步成窗口全屏，退出时恢复。
- **NSFW 是页面状态**，不改变窗口。

## 4. 转场表

| 起点 | 操作 | 终点 | 说明 |
| --- | --- | --- | --- |
| 正常 | 点“沉浸播放” | 沉浸 | 拍快照；去标题栏；禁缩放；按视频比例 fit |
| 沉浸 | `Esc` / “退出沉浸” | 正常 | 恢复快照；页面回普通布局 |
| 沉浸 | 点“全屏” / `F` | 全屏 | stage 请求 Fullscreen API |
| 正常 | 控制条全屏 / `F` | 全屏 | 壳不拍快照；系统记得原窗口 |
| 全屏 | `Esc` / 控制条退出全屏 | **正常** | `fullscreenchange=false` → `restore_normal_window()`；若之前是沉浸，恢复原始快照 |
| 全屏 | 离开页面 / 刷新 | **正常** | 同上，幂等 |
| 任意 | 离开播放页 | 正常 | unmount 兜底 |
| 浏览器 | `F` / 控制条全屏 | 元素全屏 | 没有沉浸，不碰壳 |

> 在沉浸里进全屏，退出全屏会**直接回正常**，不会回沉浸。这是按“全屏退出回原始界面”的规则定的。

## 5. `Esc` 与手动退出

`Esc` 顺序：

1. 正在全屏 → **不拦截**，交给 WebView/浏览器原生退出；`fullscreenchange` 负责回正常。
2. 目录抽屉打开 → 先关抽屉。
3. 沉浸中 → 退出沉浸，回正常。
4. 正常界面 → 什么都不做。

手动退出：

- 沉浸顶部悬浮条：**退出沉浸** / 全屏 / 关闭（无标题文字）。
- 全屏：播放器控制条的全屏按钮（四角向内）/ `Esc`。
- 统一叫“退出沉浸”，不用“还原窗口”这种容易和窗口状态混淆的文案。

## 6. 窗口快照与恢复

```text
WindowSnapshot {
  inner_size, outer_position, decorated, resizable
}

enter_player_immersive(aspect):
  if snapshot == null:
     snapshot = {
       inner_size,
       outer_position,
       decorated: window.is_decorated(),
       resizable: window.is_resizable()
     }
  window.set_decorations(false)             // 正常本来没有标题栏时是 no-op
  window.set_resizable(false)               // 正常窗口固定，沉浸也不允许系统拖边
  fit_to_aspect(aspect)                     // 严格等比例整体缩放；工作区内，保持可见
  // 重复调用只重新 fit，不覆盖快照

restore_normal_window():
  window.set_fullscreen(false)
  if snapshot != null:
     window.set_resizable(snapshot.resizable)
     window.set_decorations(snapshot.decorated)   // 正常无标题栏就保持无，不强行加标题栏
     window.set_size(snapshot.inner_size)
     window.set_position(snapshot.outer_position)
     snapshot = null
```

要点：

- **快照只在“正常 → 沉浸”时拍一次**；记录的是实际 decorations/resizable，不是假设“正常一定有标题栏、一定可缩放”。
- **正常窗口没有系统最大化**：`open_main` 明确 `.maximizable(false)`，快照不记录也不恢复 maximized；退出沉浸不会调用 `maximize()`。
- 全屏不拍壳快照：从正常进全屏，退出由系统恢复；从沉浸进全屏，快照已经是原始正常状态。
- 沉浸中程序化 `set_size` 不受 `resizable(false)` 影响；没有手动缩放热区，窗口只在进入/切视频时按比例 fit，退出仍按快照还原。
- `restore_normal_window()` 必须幂等：刷新、重复调用、全屏退出都能安全执行；失败时保留快照以便重试。
- 正常窗口原本无标题栏（`decorated=false`）时，退出沉浸仍然无标题栏；不要硬编码 `set_decorations(true)`。

## 7. 播放器全屏

- 全屏元素是内核的 `.miyabi-player-stage`，内部包含视频、控制条、GIF 面板；页面其他部分（目录、顶部条）不进入全屏。
- 控制条全屏按钮、`F` 都调 `player.toggleFullscreen()`；内核 `requestFullscreen()` / `exitFullscreen()`。
- 全屏画面 **`contain`**：完整画面 + 黑边；不裁切。
- `fullscreenchange`：内核更新控制条图标 → 页面收到 `onFullscreenChange`。
- **退出全屏时页面必须调 `restore_normal_window()`**，这样“沉浸 → 全屏 → 退出”不会停在沉浸窗口。
- Tauri 版本：Windows 的 Web fullscreen 支持从 **2.6.0** 开始（PR #13558，把 WebView2 的 `ContainsFullScreenElement` 同步成窗口全屏）。当前 `tauri = "2"` 会取最新 2.x，满足；CI 构建时确认实际版本。
- `Esc` 预期由 WebView2 原生处理；页面在 `document.fullscreenElement` 存在时不拦截。若实测不退出，再加一个只在全屏时生效的兜底，不影响普通 `Esc`。
- 从沉浸进全屏再退出时，Tauri/系统会先恢复“全屏前的沉浸窗口”，页面再调 `restore_normal_window()` 恢复原始正常窗口。两条恢复可能竞争，建议在 `fullscreenchange=false` 后用 `requestAnimationFrame`（或 `setTimeout(0)`）再发命令，让系统先完成；最终状态一定是正常。若实测直接调用也稳定，可去掉延迟。
- 浏览器没有壳命令，`restore_normal_window()` 只在 `isDesktopWindow()` 为真时调用；浏览器只处理元素全屏。
- 详情页独立播放弹窗会被移除（见第 9 节），所以全屏只需要处理播放页这一处。

## 8. 沉浸模式

### 8.1 无标题

- 进入时壳 `set_decorations(false)`；退出时 `set_decorations(snapshot.decorated)`，**按正常窗口的实际状态恢复**，不是无条件加回标题栏。
- 正常窗口本来就没有标题栏（`decorated=false`）时，沉浸和退出都保持无标题；正常窗口有标题栏时，退出才恢复标题栏。
- 不做全局无边框改造：不改正常窗口既有的标题栏状态，只在沉浸期间临时去掉（如果本来有）。
- 顶部悬浮条（hover 才出现）：
  - 退出沉浸（窗口图标）；
  - 全屏（四角向外）；
  - 关闭（✕）；
  - 空白区域 `data-tauri-drag-region`，可拖动窗口。
- 悬浮条没有标题文字，不是标题栏。

### 8.2 窗口大小

- **不做手动缩放**：`enter_player_immersive` 里 `set_resizable(false)`；窗口大小只由进入/切视频时的比例 fit 决定，退出恢复快照里的实际 `resizable`。
- 正常窗口本身固定（`resizable(false)`、`maximizable(false)`），初始尺寸取逻辑工作区的 80%，避免不同分辨率下占屏比例差异过大。
- **严格等比例**：只按视频宽高比整体缩放（宽度和高度乘同一个系数），不单独改宽或高，避免拉伸。
- 进入时按视频比例 fit：限制在当前显示器工作区内；超出时整体缩小，不裁边、不拉伸；没有元数据时先保持当前尺寸，拿到元数据再 fit。
- 正常窗口固定，不再设置最小尺寸；沉浸窗口只按视频比例 fit，程序化 `set_size` 不受 `resizable(false)` 影响；退出恢复进入前的快照。
- Windows 上无边框窗口原生边缘缩放不可靠（tauri#8519、#9268），所以不把它作为功能；程序化 `set_size` 不受影响。
- 不假设正常窗口可缩放；退出时按快照里的 `resizable` 恢复。
- 窗口仍可通过顶部悬浮条拖动。

### 8.3 画面适配

统一 **`contain`**：

| 模式 | 适配 | 说明 |
| --- | --- | --- |
| 正常 | contain | 完整画面 + 黑边 |
| 沉浸 | contain | 窗口已按视频比例 fit；min size 造成偏差时留黑边 |
| 全屏 | contain | 完整画面 + 黑边 |

删除 `coverVideo` / `sourceAspect` / `miyabi-player-cover` 相关逻辑；不再按窗口比例裁切。

### 8.4 页面布局

- 沉浸：`fixed inset-0` 黑色背景；播放器铺满；目录右侧抽屉；顶部悬浮条；`html.player-immersive` 去掉 scrollbar gutter。
- 退出：移除 fixed 布局，恢复普通播放页（播放器 + 右侧目录），不重新加载播放器，播放进度不丢。
- 页面挂载/卸载/刷新时的 `restore_normal_window()` 只在桌面壳调用；浏览器没有壳窗口状态。
- 浏览器没有沉浸模式（不能改窗口标题栏），只保留正常页 + 元素全屏；桌面端才显示“沉浸播放”入口。

## 9. 播放入口：列表与详情

### 9.1 列表（现有）

- 封面上的透明播放按钮是一个 `Link` 到 `/play?movie=<id>`；`pointer-coarse:hidden` 让触屏点卡片仍进详情。
- 封面本身由 `MediaImage` 按 NSFW 设置遮挡。

### 9.2 详情页（改）

- 在详情封面（`MovieHero` 的 3:2 图片区）加同样的透明播放按钮，点击进 `/play?movie=<id>`。
- 详情页不再渲染独立的 `MoviePlayer` 按钮/弹窗；`movie-detail/player.tsx` 删除。
- 仅当该影片有本地视频时显示播放按钮；JavDB-only 详情没有本地视频，不显示。
- 触屏：详情页没有“点封面进详情”的竞争行为，播放按钮可以一直可见（列表仍按原样隐藏）。
- 点击后 TanStack Router 跳转，详情弹窗通过现有 `onBeforeNavigate` 自动关闭。
- 这样只有 `/play` 一个播放入口，NSFW 只需要在一处处理。

### 9.3 共享组件

- 抽出 `PlayOverlay`（圆形、`border-white/30`、`bg-black/40`、`backdrop-blur`、hover/focus 淡入），列表和详情共用。
- 列表保留 `hideOnTouch`；详情不需要。
- 详情封面容器加 `relative group`；`Link` 绝对覆盖图片区，`aria-label="播放 <code>"`。

## 10. NSFW 保护与播放页

### 10.1 现状

- `MediaImage` 在 `nsfwMode` 开启时直接隐藏封面/预览图（不是模糊）。
- 播放页有两个漏洞：
  1. 目录缩略图用裸 `<img src={poster}>`，不受 `MediaImage` 控制。
  2. 播放器直接 `setSources` 并自动播放，视频画面没有任何遮挡/确认。

### 10.2 目标

**目录缩略图**

- 把裸 `<img>` 换成 `MediaImage`（或复用同一套 conceal 逻辑），和列表/详情一致。
- 无 poster 时保留“封面”文字占位。

**播放遮罩（gate）**

- 当 `nsfwMode === true && !nsfwRevealed`：
  - 不调用 `player.setSources()`；播放器保持空源，不加载、不自动播放。
  - 播放器区域显示遮罩：锁图标 + “NSFW 保护已开启” + “播放”按钮；黑底，不显示视频帧/海报。
  - 目录仍可浏览；点选影片只更新 `pending`（当前选中项），不播放。
  - 遮罩盖住控制条；隐藏/禁用“沉浸播放”和全屏入口；`F` 直接忽略。
- 点击“播放”：
  - `nsfwRevealed = true`；
  - 用 `pendingIndex` 调 `setSources(tracks, index)`；
  - 这次点击是用户手势，`video.play()` 不会被自动播放策略拦截。
  - 如果点击时播放器还没 ready，先记住 pending，在 `onReady` 后补 `setSources`。
- 切换影片：已 reveal 后正常播放；本次播放页访问内不再重复拦截。
- 离开播放页/刷新：`nsfwRevealed` 重置；下次进入重新 gate。
- `nsfwMode` 关闭：遮罩消失，恢复现在的自动播放行为。
- 如果播放中把 `nsfwMode` 打开（通常要离开播放页去设置，页面会重挂载）：`stop()` 播放器并清掉 reveal，遮罩出现；下次点击重新加载。
- 设置文案同步改成“遮挡封面与预览图，播放前需要确认”。

**详情/列表的封面**

- 已由 `MediaImage` 遮挡；封面上的播放按钮不揭图，只负责跳转。
- 跳到播放页后仍会经过 gate，所以不会“点一下就直接看到视频”。

### 10.3 已确认

- gate 粒度：**每次进入播放页确认一次**；本次访问内切换影片不再拦截，离开/刷新后重新 gate。
- 视频与缩略图**一并拦截**：NSFW 开启时，目录缩略图遮挡，视频不 `setSources`、不自动播放，点击 gate 后才加载。

## 11. 要删 / 要加

### Rust 壳

删除：

- `set_player_window_mode`、`WINDOW_BOUNDS`、`WindowBounds`、`fit_to_aspect`；
- `main.rs` / `build.rs` 的命令注册；
- `desktop-remote.json` 的 `allow-set-player-window-mode` 和未使用的 `core:window:allow-toggle-maximize`。

新增：

- `enter_player_immersive(aspect: Option<f64>)`：拍快照（尺寸/位置/实际 decorated/resizable）、`decorations(false)`、`resizable(false)`、fit；
- `restore_normal_window()`：`set_fullscreen(false)`、按快照恢复 `resizable` / `decorations` / 尺寸 / 位置；
- `WindowSnapshot { inner_size, outer_position, decorated, resizable }`；
- 权限：`allow-enter-player-immersive`、`allow-restore-normal-window`。

修改：

- `open_main` 增加 `.maximizable(false)`：**正常窗口没有系统最大化**；不添加自定义最大化按钮。
- **不强制改正常窗口的标题栏有无**：decorations 按现有/实际状态，快照负责退出时原样恢复。
- 不实现 maximized 状态；`restore_normal_window()` 不调用 `maximize()`。

保留：

- 主窗口最小 1024×640；
- `decorations` / `resizable` 的当前实际状态；
- `core:window:allow-close`、`allow-start-dragging`。

### Web 窗口/沉浸

删除：

- `window-mode.ts`、`windowMode`、`applyWindowMode`、`borderless`；
- `PlayerStage` 的 `onToggleFullscreen` 转交；内核直接走元素全屏；
- `coverVideo` / `sourceAspect` / `cover` prop / `miyabi-player-cover`；
- `setFullscreenState(windowMode === 'fullscreen')` 这类由窗口模式驱动的图标同步。

改名/新增：

- `borderless` → `immersive`；`html.player-borderless` → `html.player-immersive`；
- `enterImmersive()` / `exitImmersive()`；
- `fullscreenchange` 退出时：`setImmersive(false)` + `restoreNormalWindow()`；
- 沉浸顶部悬浮条（退出/全屏/关闭）。

### Web 详情/播放

- 新增共享 `PlayOverlay`；列表复用；详情封面加播放 `Link`。
- `MovieHero` 增加 `playMovieId?`；`LibraryMovieDetail` 传 `videos.length ? movieId : undefined`。
- `content.tsx` 删除 `MoviePlayer`；删除 `movie-detail/player.tsx`。
- 播放页目录缩略图换 `MediaImage`。
- 播放页加 NSFW gate（状态、遮罩、pending、F/沉浸/全屏拦截）。
- `privacy-section.tsx` 文案更新。
- 如测试覆盖详情播放按钮，补充/调整。

### 文档

- 本文作为方案；实现后回填 `MODIFICATIONS.md`。

## 12. 验收清单

### 窗口/沉浸/全屏

1. 正常窗口（无最大化）→ 沉浸 → 退出：尺寸、位置、实际标题栏状态（有/无）完全一致。
2. 正常窗口没有最大化按钮/状态：`maximizable(false)` 生效；进入沉浸前后都不出现最大化。
3. 沉浸无标题栏；悬浮条可拖动；不可手动缩放；退出按钮可见。
4. 沉浸窗口的宽高比与视频一致：fit 前后不拉伸；min size/工作区冲突时只整体缩放，不单独改某一边。
5. `Esc` 退出沉浸 → 正常；顶部按钮退出 → 正常。
6. 正常 → 全屏 → `Esc`：回正常，尺寸/标题栏状态不变；画面 `contain`。
7. 沉浸 → 全屏 → `Esc`：**回正常**，不是回沉浸；窗口恢复进入沉浸前的状态（尺寸/位置/decorations/resizable）。
8. 沉浸 → 全屏 → 控制条退出全屏：同上。
9. 全屏中切上一部/下一部/自动连播：仍在全屏；不再触发窗口模式切换。
10. 刷新/导航时处于沉浸或全屏：回到正常，页面状态一致。
11. 双显示器：沉浸 fit 在当前显示器；全屏在当前显示器；退出回原位置。
12. CI 构建的 Tauri 版本 ≥ 2.6，Windows 全屏实际生效。

### 详情播放入口

13. 有本地视频的详情：封面上出现透明播放按钮；点击进 `/play?movie=<id>`，详情弹窗关闭。
14. JavDB-only 详情：没有播放按钮。
15. 触屏：详情播放按钮可见；列表仍点封面进详情。
16. 详情不再出现独立的“播放”按钮/弹窗。

### NSFW

17. NSFW 开启：播放页目录缩略图被遮挡；播放器区域显示 gate，无视频帧/声音。
18. gate 下点目录其他影片：只更新选中项，不播放。
19. 点 gate 的“播放”：开始播放；本次访问内切影片不再拦截。
20. 刷新播放页：重新 gate。
21. NSFW 关闭：目录缩略图可见；进入播放页自动播放（现有行为）。
22. 详情/列表封面仍按 NSFW 设置遮挡；封面上的播放按钮不揭图。
23. 触屏/浏览器同样生效。

## 11.5 UI 配置持久化

**事实**：Emby / 网络 / 本地源 / 115 / 访问密码 / 数据目录 / LAN 都是服务端或 `desktop.json` 的真持久化；只有设备 UI 偏好曾经走 localStorage。

**为什么看起来没持久化**：非 LAN 模式每次启动随机选 loopback 端口（`sidecar.rs::free_port`），localStorage 按 `协议://host:port` 隔离；端口一变就是新 origin，NSFW/主题/音量/倍速全部回默认。这不是配置没存，是存错了作用域。

**方案**：同目录 `ui-state.json`：

```json
{
  "nsfwMode": false,
  "theme": "system",
  "player": { "volume": 1, "muted": false, "rate": 1 },
  "gif": { "fps": 10, "scale": 75, "loop": "infinite" }
}
```

- 壳命令 `load_ui_state` / `save_ui_state`；写入用临时文件 + rename，避免半截 JSON。
- `main.tsx` 在 `createRoot` 前 `await loadUiState()`：NSFW 不闪、播放器创建时音量/倍速就是对的、主题不闪。
- `stores/ui-state.ts` 是唯一来源；`useUiState` 用 `useSyncExternalStore`，保存 250ms 防抖 + `pagehide` flush。
- 浏览器没有 Tauri 时回退 `localStorage['miyabi-ui-state']`，并一次性迁移旧键。
- 控制条不再单独存 `playbackRate`，倍速只走 `ui-state.player.rate`；GIF 面板经 `window.MiyabiUiState` 桥接。
- 升级后第一次启动旧随机端口下的值读不到，需要重新关一次 NSFW/设一次主题；之后不再丢。

## 12. 实现记录（0.4.14）

已按本文落地：

- 壳：`enter_player_immersive` / `restore_normal_window` 取代 `set_player_window_mode`；主窗口与 setup 都 `.maximizable(false)`；快照记录实际 `decorated` / `resizable`，不涉及最大化。
- 全屏：内核直接 `requestFullscreen()`，`PlayerStage` 不再转发；退出时页面 `restoreNormalWindow()`。
- 沉浸：`windowMode` 三态删除，改为 `immersive` 布尔；`player-borderless` 改名 `player-immersive`；`cover` 逻辑删除，统一 `contain`；按视频比例 fit、不提供手动缩放。
- 布局：播放器外层 wrapper 改为 `relative flex min-h-0 flex-1 flex-col`，修复 NSFW 遮罩包一层后播放器缩成 16:9 靠顶部的问题。
- 持久化：新增 `ui-state.json` + `load_ui_state` / `save_ui_state`；`main.tsx` 渲染前预加载；NSFW / 主题 / 播放器 / 目录 / GIF 偏好不再依赖 localStorage 的 origin。
- 详情：新增 `PlayOverlay`，列表复用；详情封面加播放按钮；删除 `movie-detail/player.tsx`。
- NSFW：播放页缩略图换 `MediaImage`；视频 gate（每次进入播放页确认一次）。
- 验证：`tsc --noEmit`、`oxlint`、`oxfmt --check`、`web test`（198）、`web build` 通过；Rust 由 CI 编译。

## 13. 本轮已确认

- 沉浸不做手动拖边缩放，只由壳严格按视频宽高比等比例 fit。
- 全屏 `contain`。
- 详情页独立播放弹窗删除，统一走 `/play?movie=<id>`；封面用和列表一样的透明播放按钮。
- 退出回“进入沉浸前的播放页 + 原始窗口状态”（尺寸/位置/实际 decorations/实际 resizable；不涉及最大化）。
- 正常窗口固定（`resizable(false)`、`maximizable(false)`），初始尺寸为逻辑工作区的 80%。
- 右侧目录固定 320px，点击手柄只有“展开 / 收起”两个状态，不再拖拽调宽。
- **正常窗口没有系统最大化**：`open_main` 明确 `.maximizable(false)`；壳不实现最大化，快照不记录 maximized，退出不调用 `maximize()`；也不强行加/去标题栏。
- NSFW 补到播放页：目录缩略图遮挡 + 视频 gate；gate 每次进入播放页确认一次，本次访问内不再重复拦截；视频和缩略图一并拦截。

> “壳只能等比例调整大小”按“沉浸窗口由壳调整时严格保持视频宽高比”理解；正常窗口固定，不可缩放。
