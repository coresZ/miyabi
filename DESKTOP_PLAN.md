# Miyabi 桌面版（Wails）一步到位方案文档

版本：1.0
日期：2026-09-24
状态：可行性已核验（基于 `master` / `v1.3.0` 分支当前代码）
适用目标：Windows 10/11 amd64，产出可发布的 `miyabi-desktop.exe`（无系统浏览器依赖、无控制台黑框），由 GitHub Actions 构建。

---

## 0. 结论摘要（TL;DR）

整体**可行**，但原文有 3 处必须修正的技术判断，否则会"落地即踩坑"：

1. **不要用 Wails AssetServer 反向代理 `/api`（原文 §5 方案 A）。**
   Wails v2（当前 2.15.0）官方能力矩阵明确：Windows 下 AssetServer 的 `Response Body Streaming = ❌`。也就是说，经 Wails 资源服务器转发的响应体无法流式输出，会**破坏 `/api/tasks/events` 的 SSE**（任务进度实时推送）。
   → 改用**方案 B 的加强版**：WebView 由 AssetServer 的 302 重定向直接加载本机 Gin 服务 `http://127.0.0.1:<port>`。同源、Cookie、SSE、流式播放全部原生可用，且**前端零改动**。

2. **Wails v2 没有内置系统托盘。**
   v2 只有"应用菜单"（`Menu` / `MenuSetApplicationMenu`，语义偏 macOS），没有托盘图标 API。原文 §7 的"关窗口 → 托盘"需要第三方 systray 库，或降级为"最小化到任务栏 + 设置页退出按钮"。

3. **`MIYABI_LISTEN=127.0.0.1:0` 与 `PublicURL` 存在构造期冲突。**
   `internal/app/app.go` 在 `New()` 阶段就把 `cfg.PublicURL` 烘焙进 STRM/Emby 导出（`scrapeSvc.SetEmbyExport(cfg.EmbyDir, cfg.PublicURL, ...)`，app.go:148/152）。若用随机端口 `:0`，`PublicURL` 无法在构造前确定；且端口每次漂移会导致历史 `.strm` 文件里的地址失效。
   → 桌面版使用**固定 loopback 端口（建议 `127.0.0.1:18765`）**，启动前预探测占用；而不是 `:0`。

其余结论：
- 前端**已满足**"相对路径"要求，无需改 API 基址（详见 §5.1 证据）。
- 需新增依赖 `github.com/wailsapp/wails/v2`（与全局约定"禁止自行安装依赖"冲突，**需你批准**）。
- `internal/app` 需要小幅增强（暴露可注入 listener 的 `RunListener` 与可取消生命周期），不改业务逻辑。
- 控制台版 `cmd/miyabi`、Docker 入口完全保留，互不影响。

---

## 1. 目标与成功标准

### 1.1 产品目标

| 项 | 要求 |
| --- | --- |
| 启动 | 双击 `miyabi-desktop.exe` 只出现应用窗口 |
| 浏览器 | 不依赖 Chrome / Edge（使用系统 WebView2 运行时） |
| 功能 | 与现有 Web 版一致：115、刮削、订阅、SSE、播放 / STRM 等 |
| 数据 | 默认 `exe 同级 data/`，可与控制台版共用（不同时运行） |
| 退出 | 关窗口或托盘退出后进程干净结束，SQLite 正常关闭 |
| 发布 | Tag 后自动产出 `miyabi-desktop-windows-amd64.zip` |

### 1.2 非目标（本阶段不做）

- 不把业务 API 全部改成 Wails Binding（见 §6 说明：本方案甚至不需要 JS Binding）。
- 不重写前端框架。
- 不删除 `cmd/miyabi` 控制台 / Docker 入口。
- 不做自动更新框架。
- 不做非 Windows 平台的桌面包（`cmd/miyabi-desktop` 用 `//go:build windows` 约束，见 §6.0）。

---

## 2. 架构（修正后）

```text
miyabi-desktop.exe
├── Wails Runtime（桌面壳，仅四件事）
│   ├── WebView2 窗口（无系统浏览器、无控制台）
│   ├── 单实例锁 options.SingleInstanceLock（内置，无需自研）
│   ├── 生命周期 OnStartup / OnBeforeClose / OnShutdown
│   └── AssetServer：把首个请求 302 重定向到本机 Gin
└── 同进程业务（internal/app，原样复用）
    ├── Gin @ 127.0.0.1:18765
    ├── SQLite / worker / pan / javdb / catalogue / ...
    └── 前端静态资源：Gin installFrontend(web/dist 嵌入) 直接托管
```

流量约定（关键变化）：

- **UI、数据、SSE 全部同源**：WebView 最终地址是 `http://127.0.0.1:<port>`，由 Gin 同时提供前端与 `/api/*`。
- 前端相对路径 `/api/...` 与相对 `EventSource` 无需任何改动。
- Wails 不再代理业务流量，因此绕开 Windows 流式响应限制。

对比原文：
- 原文 `AssetServer（web/dist）` + `Bindings` 的职责，被替换为 `302 → Gin`；`Bindings` 被替换为桌面模式下的少量 Gin 端点（见 §6.3）。

---

## 3. 目录与文件清单

```text
miyabi/
├── cmd/
│   ├── miyabi/                    # 保留：控制台
│   └── miyabi-desktop/
│       ├── main.go                # //go:build windows；wails.Run 入口 + 302 重定向
│       └── app.go                 # DesktopApp：启动/停止后端、单实例回调、托盘/关闭策略
├── internal/
│   ├── app/                       # 现有；新增 RunListener / 可取消生命周期（不破坏 Run）
│   ├── config/                    # 新增桌面默认值（LoadDesktop / Mode）
│   └── desktop/
│       ├── paths.go               # exe 目录、默认 data 路径
│       └── mode.go                # RuntimeDesktop 标记、桌面专用端口常量
├── internal/api/                  # 新增桌面端点（仅 desktop 模式注册，见 §6.3）
├── web/                           # 现有 Vite；无需改 API 基址
├── wails.json
├── go.mod                         # + wails v2（需批准）
└── .github/workflows/
    └── release-binaries.yml       # 控制台 + 桌面合并为一个 workflow、单一 release job
```

相对原文的变化：
- **删除** `internal/desktop/singleinstance.go`：Wails v2 内置 `options.SingleInstanceLock`，自研文件锁是多余且易错的。
- **删除** `cmd/miyabi-desktop/bind.go`：本方案 WebView 由 Gin 托管，Wails 的 JS Binding 不会注入，改用 Gin 端点。
- **合并** CI workflow：原文 `release-desktop.yml` 独立文件会与 `release-binaries.yml` 在同一 tag 下并发创建/更新 Release，产生竞态；改为同一 workflow 增加 `build-desktop` job + 单一 `release` job（见 §11）。

---

## 4. 配置约定

### 4.1 桌面模式默认

| 变量 / 项 | 桌面默认 | 说明 |
| --- | --- | --- |
| `MIYABI_LISTEN` | `127.0.0.1:18765` | 仅本机；固定端口（见 §0.3） |
| `MIYABI_DATA_DIR` | `{exeDir}/data` | 未设置时 |
| `MIYABI_PUBLIC_URL` | `http://127.0.0.1:18765` | STRM 本机地址，须与 LISTEN 一致 |
| `MIYABI_ACCESS_PASSWORD` | 可为空 | 空 = 无门禁（仅本机 loopback，可接受）；非空则同现网 |
| Runtime Mode | `desktop` | 与 `server` 区分日志、监听、桌面端点注册 |

实现方式：`cmd/miyabi-desktop` 在调用 `config.Load()` 之前注入上述环境变量默认值（仅当用户未显式设置），再调用 `config.Load()`，最后把 `cfg.Mode` 标为 desktop。**不要**在 `config.Load()` 里硬编码桌面逻辑，避免污染控制台/Docker 行为。

端口占用处理：启动时先 `net.Listen("tcp", "127.0.0.1:18765")` 预探测：
- 成功 → 关闭探测 listener，交给 `app` 正式绑定（或直接复用，见 §6.5 的 `RunListener`）。
- 失败（被占用）→ 弹 Wails `MessageDialog` 报错并退出，**不静默换端口**（否则历史 STRM 失效）。单实例锁已保证正常情况不会自占端口。

### 4.2 控制台模式

行为保持现状：可 `0.0.0.0:8080`、Docker 环境变量等，全部不变（config.go:32-91 现状即符合）。

---

## 5. 前端与 API 同源方案（必做，含证据）

### 5.1 前端现状：已满足，无需改动

核验证据（当前代码）：
- `web/src/api/client.ts:98` 使用相对路径 `fetch(path)`；`:18` 用 `new URL(path, window.location.origin)`；`:88` 图片走 `/api/image`。
- `web/src/features/tasks/task-events.tsx:43` 使用相对 `new EventSource('/api/tasks/events')`。
- 全仓库前端**无** `VITE_API_BASE`、无硬编码 `http://127.0.0.1:8080`（grep 结果为空）。

因此 §5.1 的"所有请求改为相对路径"这一项**已经完成**，无需再改。

### 5.2 后端桥接：二选一，选 B（原文方案 A 不可行）

**方案 A（原文推荐）：AssetServer 代理 `/api` → ❌ 否决。**
- 依据：Wails v2.15 官方 Options 文档的 AssetServer 能力矩阵：`Response Body Streaming` 在 **Windows = ❌**。
- 后果：`/api/tasks/events`（`internal/api/sse.go` 的 `text/event-stream`，含 15s 心跳）经 Wails 资源服务器转发时会被缓冲，SSE 失效；`/api/play/:id/stream/:resource` 的流式播放也会受影响。

**方案 B（推荐）：302 重定向到本机 Gin → ✅ 采用。**
- WebView 的初始 GET 由 Wails AssetServer 的 `Handler` 处理，返回 `302` 指向 `http://127.0.0.1:<port>`；此后所有请求（HTML、JS、`/api/*`、SSE、流媒体）**直连 Gin**，不再经过 Wails。
- 同源：`http://127.0.0.1:<port>` 一个 origin 同时提供前端与 API，Cookie（`miyabi_token`）、`EventSource`、`Authorization` 全部原生工作。
- 不触碰 Windows 流式限制。
- 代价：Wails 的 JS 运行时（`window.runtime` / `window.go`）不会注入到 Gin 托管的页面。**当前前端未使用任何 Wails JS**（grep 无 `window.runtime`），故无影响；桌面专用动作改走 Gin 端点（§6.3）。

示意（`cmd/miyabi-desktop/main.go`）：

```go
//go:build windows

package main

import (
	"net/http"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	publicURL := resolveDesktopURL() // http://127.0.0.1:18765
	app := NewDesktopApp(publicURL)

	err := wails.Run(&options.App{
		Title:             "Miyabi",
		Width:             1280,
		Height:            800,
		MinWidth:          960,
		MinHeight:         600,
		HideWindowOnClose: false, // 关闭策略见 §7
		AssetServer: &assetserver.Options{
			// 不设置 Assets；所有 GET 落到 Handler
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, publicURL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			}),
		},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "miyabi-desktop-<固定 UUID>",
			OnSecondInstanceLaunch: app.onSecondInstance,
		},
	})
	if err != nil {
		app.fatal(err)
	}
}
```

> 注意：302 重定向后 WebView 地址变为 `http://127.0.0.1:<port>`，属正常顶层导航（Wails 能力矩阵 `HTTP Redirects 30x` 在 Windows = ✅）。实施时需实测确认 WebView2 允许该顶层跳转；若不允许，回退方案见 §14。

---

## 6. 模块职责

### 6.0 构建约束

`cmd/miyabi-desktop/*.go` 统一加 `//go:build windows`：
- 避免 Linux/macOS 上 `go build ./...` / `go test ./...` 因 Wails（Linux 需 WebKitGTK 头文件）而失败。
- 本方案只交付 Windows 桌面包，约束合理。

### 6.1 `cmd/miyabi-desktop/main.go`

- 注入桌面默认环境变量 → `config.Load()` → 标记 desktop 模式。
- 解析 `{exeDir}` 与端口，预探测占用。
- 构造 `DesktopApp`，`wails.Run`：标题、尺寸、AssetServer 重定向、单实例、生命周期。
- 启动失败（端口/DB）时用 `runtime.MessageDialog` 显示错误再退出，避免"无界面静默失败"。

### 6.2 `DesktopApp`（app.go）

| 方法 | 行为 |
| --- | --- |
| `startup(ctx)` | 记录 Wails ctx；`go app.RunListener(ctx, ln)` 启动 Gin + workers；失败则 dialog + `runtime.Quit` |
| `beforeClose(ctx) bool` | 关闭策略（§7）。默认返回 `false`（允许关闭），由 `shutdown` 做优雅退出 |
| `shutdown(ctx)` | cancel 后端 ctx → 等 `RunListener` 返回（内部 10s 优雅关 HTTP + 等 workers）→ `app.Close()` 关 DB |
| `onSecondInstance(data)` | `runtime.WindowShow` + `WindowUnminimise` + `WindowSetAlwaysOnTop(true→false)` 激活已有窗口 |
| `fatal(err)` | 写 `{data}/desktop.log` + dialog |

### 6.3 桌面动作（替代原文 Bindings）

因页面由 Gin 托管，Wails JS Binding 不可用；桌面专用能力改为**仅在 desktop 模式注册**的 Gin 端点。实现上给 `api.Dependencies` 增加一个可选 `Desktop DesktopHooks`（server 模式为 `nil`，端点不注册）。

| 端点 | 用途 | 备注 |
| --- | --- | --- |
| `POST /api/desktop/reveal-data` | 资源管理器打开 data 目录 | `exec.Command("explorer", dataDir)` |
| `POST /api/desktop/quit` | 优雅退出（设置页"退出"按钮用） | 触发 cancel + `runtime.Quit` |
| `GET /api/desktop/version` | 版本/关于 | 也可并入现有 `/api/settings/system` |

- `SelectDataDir`：**不做**。桌面数据目录固定为 `{exeDir}/data`，改目录需重启，收益低、且原生目录选择依赖 Wails dialog（当前页面拿不到）。如确需，后续再评估。
- `OpenExternal`：**暂不做**。当前前端无外链跳转代码（grep 无 `window.open`/`location.href`/`<a target=_blank>`），115 登录走二维码/轮询而非外链。若后续需要，再加白名单校验的 `POST /api/desktop/open-url`。
- 业务刮削等**不**进桌面端点，保持 API 统一。

### 6.4 `internal/desktop`

- `paths.go`：`ExecutableDir()`（`os.Executable` → `filepath.Dir`）、`DefaultDataDir()`。
- `mode.go`：`RuntimeDesktop` 常量、桌面默认端口常量 `DefaultListen = "127.0.0.1:18765"`。
- 单实例：由 Wails `SingleInstanceLock` 负责，本包不实现。

### 6.5 `internal/app` / `config` 改动要点（最小侵入）

现状（app.go:211-260）：`Run(ctx)` 内部 `ListenAndServe()`，无法拿到实际端口，也无法复用外部 listener。

建议：
1. 抽出 `func (a *App) RunListener(ctx context.Context, ln net.Listener) error`，`Run(ctx)` 变成"`net.Listen(cfg.Listen)` + `RunListener`"。桌面版预绑定 `ln`（端口确定）后调用 `RunListener`。
2. 生命周期保持"cancel ctx → 优雅关闭"语义不变；桌面 `shutdown` 只需 cancel 并等待。
3. `config` 增加 `Mode`（`RuntimeServer` / `RuntimeDesktop`）字段，`Load()` 默认 server；桌面入口负责覆盖。
4. 桌面模式可关闭 Gin 的静态前端路由（可选）：因为 Wails 不代理，Gin 仍需提供前端，故**保留** `installFrontend`。此条与原文相反——原文想让 Wails 托管静态，本方案由 Gin 托管。

---

## 7. 窗口与托盘行为（修正）

**前提：Wails v2 无内置托盘。**

| 用户操作 | 方案 7A（推荐，无新依赖） | 方案 7B（需新依赖） |
| --- | --- | --- |
| 点开 exe | 单实例；显示主窗口 | 同左 |
| 关窗口 | 直接优雅退出（`beforeClose` 返回 false） | 隐藏到托盘 |
| 最小化 | 任务栏最小化（系统默认） | 托盘 |
| 退出 | 关窗口即退出；或设置页"退出"按钮 | 托盘菜单"退出" |
| 系统关机 | `OnShutdown` 优雅关闭 | 同左 |

- **7A 推荐理由**：零新依赖、行为可预期，不会出现"隐藏后找不回"。若确实要"关窗口 = 最小化到任务栏"，可在 `beforeClose` 里 `runtime.WindowMinimise(ctx)` 并 `return true`，再配合设置页"退出"按钮。
- **7B 托盘方案**：需引入第三方 systray（候选：`getlantern/systray`、`fyne.io/systray`、`energye/systray`）。注意 Windows 下 systray 消息循环与 Wails 主循环存在线程竞争，需要 `runtime.LockOSThread` 或选用对 Wails 友好的实现，属于**中等风险**，需你批准新增依赖。
- 原文"建议默认：关窗口 → 托盘；托盘退出 → 真退出"在 7B 下成立；本方案默认采用 7A，把托盘列为可选项。

---

## 8. 生命周期时序

```text
main
  desktop.Paths / config 默认值
  端口预探测（127.0.0.1:18765）
  cfg = config.Load()  → Mode=desktop, Listen/PublicURL=固定 loopback
  backend = app.New(cfg)           // 组装 Gin/SQLite/workers
  dapp = NewDesktopApp(backend, publicURL)
  wails.Run(
    SingleInstanceLock: 已存在实例 → onSecondInstance 激活后 os.Exit(0)
    OnStartup:   go backend.RunListener(ctx, ln)   // Gin + workers
    OnBeforeClose: 关闭策略（§7）
    OnShutdown: cancel ctx → 等 RunListener 返回 → backend.Close()
  )
启动失败：MessageDialog + 写 desktop.log，再退出
```

与原文差异：不再有 `AssetServer 托管 web/dist`，改为 302 → Gin；`Bindings` 改为 Gin 端点。

---

## 9. wails.json

```json
{
  "name": "Miyabi",
  "outputfilename": "miyabi-desktop",
  "frontend:dir": "web",
  "frontend:install": "pnpm install --frozen-lockfile",
  "frontend:build": "pnpm build",
  "frontend:dev:watcher": "pnpm dev",
  "frontend:dev:serverUrl": "auto",
  "author": { "name": "Miyabi" },
  "info": {
    "companyName": "Miyabi",
    "productName": "Miyabi",
    "productVersion": "1.3.0"
  }
}
```

说明：
- 字段以所用 Wails 版本（建议 v2.15.0）为准。
- **构建不走 Wails CLI**（其要求 main 包在项目根），`wails.json` 仅作配置留存/未来工具链使用；实际由 CI 手动 `go build ./cmd/miyabi-desktop`（§11.2）。
- `frontend:build` 产出的 `web/dist` 供 Gin 的 `embed.go`（`//go:build !dev`）嵌入，是运行时的真实前端来源。
- 本方案不使用 JS Binding，`wailsjsdir` 可省略。

---

## 10. 实施顺序（纯代码 + CI 构建，本地无需 Go/Wails 工具链）

前提：本地只编辑代码，**不安装** Wails CLI / Wails 依赖；所有编译与验证在 GitHub Actions 完成。
本地 Go（`go1.27.0`）可选，仅用于编译非桌面包与跑测试，不拉取 Wails。

1. 后端增强：`internal/config` 增加 `Mode`；`internal/app` 抽出 `RunListener` 与 `WithDesktop`。
2. 新增 `internal/desktop`（`paths.go`：exe 目录、默认 data、固定端口、本机 URL）。
3. 新增 `cmd/miyabi-desktop`：
   - `main.go` 用 `//go:build windows && miyabidesktop` 约束，导入 Wails；
   - `main_stub.go` 用 `//go:build !windows || !miyabidesktop`，保证无 Wails 依赖时 `go build ./...` / `go test ./...` 仍通过。
4. 新增 `wails.json`。
5. 桌面端点（`internal/api`，仅 desktop 模式注册）。
6. 前端保持相对路径（已满足，回归验证即可）。
7. 提交后由 CI（Windows）构建并验收；本地最多跑 `go build ./cmd/miyabi` 与 `go test ./...`（不含 `miyabidesktop` tag）。
8. 回填本目录 `MODIFICATIONS.md`。

依赖解析约定：仓库 `go.mod` **不提交** `wails` require；由 CI 桌面 job 在构建前 `go get github.com/wailsapp/wails/v2@v2.15.0`（或 `wails build` 默认的 mod sync）现场解析。这样本地、控制台、Docker 构建均不受影响。
副作用：编辑器中 `cmd/miyabi-desktop/main.go` 会因缺少 wails 模块而报诊断，属预期，可忽略。

---

## 11. GitHub Actions（修正：合并为单一 workflow）

### 11.1 要点

- 桌面 job 用 `runs-on: windows-latest`（**不要**在 Ubuntu 交叉编 Wails）。
- 先 `pnpm build` 再构建桌面（Wails 也会跑 `frontend:build`，可幂等）。
- 桌面入口由 `miyabidesktop` 构建 tag 开启；构建参数补齐 Wails 的 `desktop,production` 生产标签。
- **不提交** `wails` 依赖：job 内 `go get github.com/wailsapp/wails/v2@v2.15.0` 现场解析。
- WebView2 策略：默认 `download`（首启联网）；离线分发用 `-webview2 embed`。按需选择。
- **不要**新建独立 `release-desktop.yml`：同一 tag 下两个 workflow 会并发创建/更新同一个 Release，产生竞态。改为在 `release-binaries.yml` 增加 `build-desktop` job，并让 `release` job `needs: [build, build-desktop]`。
- 产物命名：控制台 `miyabi-windows-amd64.zip`；桌面 `miyabi-desktop-windows-amd64.zip`。

### 11.2 `build-desktop` job 步骤清单

1. `actions/checkout@v4`
2. `pnpm/action-setup@v4`（version 10）+ `actions/setup-node@v4`（node 22，cache pnpm）
3. `web`: `pnpm install --frozen-lockfile && pnpm build`
4. `actions/setup-go@v5`（`go-version-file: go.mod`，cache true）
5. 解析依赖：`go get github.com/wailsapp/wails/v2@v2.15.0 && go mod tidy`
6. **手动构建**（Wails CLI 要求 main 包在项目根目录，而桌面入口在 `./cmd/miyabi-desktop`，故采用 Wails 官方 Manual Builds 的等价生产参数，不安装 Wails CLI）：
   `CGO_ENABLED=0 go build -tags "miyabidesktop desktop production" -trimpath -ldflags "-w -s -H windowsgui" -o miyabi-desktop.exe ./cmd/miyabi-desktop`
   - `-H windowsgui` 去掉控制台黑框；无 `.syso` 时图标为默认、清单缺失，属可接受的取舍。
7. 组装目录：`miyabi-desktop.exe` + `README.txt`（§12）
8. `Compress-Archive` 打成 `miyabi-desktop-windows-amd64.zip`
9. `actions/upload-artifact@v4`；由合并后的 `release` job 统一 `softprops/action-gh-release@v2` 挂载 `artifacts/*.zip`

### 11.3 资产命名

| 文件 | 含义 |
| --- | --- |
| `miyabi-windows-amd64.zip` | 控制台版（现有） |
| `miyabi-desktop-windows-amd64.zip` | 桌面版（本文） |

---

## 12. README（随桌面压缩包分发）

```text
Miyabi Desktop

1. Unzip to any folder
2. Double-click miyabi-desktop.exe
3. Data folder: .\data (auto-created)
4. Optional: set MIYABI_ACCESS_PASSWORD before start

Do not delete the data folder if you want to keep library state.
Console edition zip is separate; both can share the same data folder
if pointed to the same path (do not run both at once).
```

（项目根 `README.md` 不改；此文本只放进桌面压缩包。）

---

## 13. 测试清单（一步到位验收）

- [ ] 双击启动仅窗口，无控制台黑框
- [ ] 门禁 / 登录、115 登录、浏览列表
- [ ] 刮削与 SSE 进度（**重点**：验证 §5.2 方案 B 下 SSE 正常）
- [ ] 播放 / STRM 相关路径本机可用（含流式响应）
- [ ] 关窗口行为符合 §7 选定策略；退出后无残留进程
- [ ] 第二次启动激活已有实例，不双开锁库
- [ ] 杀进程后再开，DB 无损坏
- [ ] `MIYABI_ACCESS_PASSWORD` 设置后门禁生效
- [ ] 干净 Windows（无 WebView2 预装）首启可运行（对应 `-webview2` 策略）
- [ ] CI 产物在另一台 Windows 可运行

---

## 14. 风险与固定对策

| 风险 | 对策 |
| --- | --- |
| Wails AssetServer 代理破坏 SSE/流式 | 采用 302 → Gin（方案 B）；实施时必测 SSE 与播放流 |
| 302 顶层跳转被 WebView2 拦截（低概率） | 回退：`AssetServer.Handler` 做反向代理（非流式接口可用），SSE 改前端轮询兜底；或评估 Wails v3 |
| 端口漂移导致历史 STRM 失效 | 固定 `127.0.0.1:18765`；占用时报错退出而非换端口 |
| SQLite 双开 | Wails `SingleInstanceLock` 强制单实例 |
| 关窗杀任务 | §7A：关闭即优雅退出（10s 超时）；如需后台常驻再上托盘 |
| Wails v2 无托盘 | 默认 7A 无托盘；托盘作为可选 7B（第三方依赖，需批准） |
| 新增 wails 依赖违反约定 | 已获批准；仓库 `go.mod` 不提交，CI 现场 `go get`，本地不安装 |
| `go test ./...` 因缺 Wails 失败 | `cmd/miyabi-desktop/main.go` 用 `windows && miyabidesktop` 约束，默认走 `main_stub.go` |
| Wails CLI 要求 main 包在项目根 | 改用 Wails 官方 Manual Builds 等价参数手动 `go build` |
| 桌面与 Docker/控制台混淆资产名 | 资产名带 `desktop` 后缀 |
| 首个无 WebView2 的机器联网受限 | 用 `-webview2 embed` |
| 两 workflow 并发写 Release | 合并为单一 workflow + 单一 release job |

---

## 15. 实施完成定义（DoD）

- 仓库存在可维护的 `cmd/miyabi-desktop` 与 `wails.json`（windows 构建约束）。
- `internal/app` 支持注入 listener 与可取消生命周期，业务行为不变。
- 本地 `wails build` 产物满足 §1 成功标准，且 §13 全部通过。
- CI 在 tag 下产出并挂载 `miyabi-desktop-windows-amd64.zip`。
- 控制台版 / Docker 版行为与产物不受影响。
- 本方案文档、`MODIFICATIONS.md`、Release 说明一致；用户不依赖系统浏览器。

---

## 16. 总结

一步到位的本质是**职责切分**：

- **Wails v2 只做桌面壳**：窗口、单实例、生命周期、无控制台构建。
- **Gin 做全部 HTTP**：前端静态、`/api/*`、SSE、流媒体，天然同源、天然可流式。
- **WebView 通过 302 直达 Gin**，从而规避 Wails AssetServer 在 Windows 上不支持响应体流式的硬限制。
- 前端相对路径已就绪，**零改动**；`internal/app` 只做最小增强。
- 托盘不是 Wails v2 能力，按 §7 决策；CI 合并为单一 workflow 避免 Release 竞态。

按 §10 顺序实现，用 §13 验收，用 §11 发布即可。

---

## 附录 A：可行性核验证据（代码位置）

| 结论 | 证据 |
| --- | --- |
| 前端已用相对路径 | `web/src/api/client.ts:18,88,98`；`web/src/features/tasks/task-events.tsx:43` |
| 无硬编码 API 基址 / 无 VITE_API_BASE | 全前端 grep 无匹配 |
| 前端未用 Wails JS 运行时 | grep 无 `window.runtime` / `window.go` |
| SSE 实现 | `internal/api/sse.go:10-47`（`text/event-stream` + 15s 心跳） |
| SSE 路由 | `internal/api/router.go:115` `GET /tasks/events` |
| 认证支持 header/cookie/query | `internal/api/auth.go:143-161`（query `?token=` 亦可） |
| Gin 已托管嵌入前端 | `internal/api/router.go:151-176`；`embed.go:10-18`（`//go:build !dev`） |
| `PublicURL` 构造期烘焙 | `internal/app/app.go:148,152` |
| `Run` 内部自行监听、无实际端口回传 | `internal/app/app.go:211-236` |
| 配置项与默认值 | `internal/config/config.go:32-91` |
| 现有控制台/Docker 构建 | `.github/workflows/release-binaries.yml`；`Dockerfile` |
| 当前版本 | `web/package.json` 0.1.2；tag `v0.1.2` |
| Wails v2 无托盘 / 有应用菜单 | Wails v2.15 文档：Menu（`MenuSetApplicationMenu`，JS 不支持） |
| Wails AssetServer Windows 不支持响应体流式 | Wails v2.15 Options 文档：AssetServer 能力矩阵 `Response Body Streaming ❌ Win` |
| Wails 内置单实例 | Wails v2.15 Options 文档：`SingleInstanceLock{ UniqueId, OnSecondInstanceLaunch }` |
| Wails 无控制台构建 | Wails v2.15 CLI 文档：`-windowsconsole` 才是"保留控制台"，默认无 |

## 附录 B：相对原文的修订点

1. §5.2：方案 A（AssetServer 代理）→ 改为方案 B（302 直连 Gin），因 Windows 流式限制。
2. §3：删除 `internal/desktop/singleinstance.go`（用 Wails 内置）与 `bind.go`（改 Gin 端点）。
3. §4.1：`MIYABI_LISTEN` 由 `:0` 改为固定 `127.0.0.1:18765`（`PublicURL` 构造期冲突）。
4. §6.3：Bindings → desktop 模式 Gin 端点；`SelectDataDir`/`OpenExternal` 暂不做。
5. §6.5：桌面静态由 **Gin** 托管（原文让 Wails 托管，方向相反）。
6. §7：明确 Wails v2 无托盘，给出 7A/7B 两方案。
7. §11：合并 workflow，避免 Release 竞态；补充 `-webview2 embed` 说明。
8. 新增 §6.0：`cmd/miyabi-desktop` 加 `//go:build windows`。

## 附录 C：待你确认的决策

1. **新增依赖** `github.com/wailsapp/wails/v2`（约 v2.15.0）——是否批准？（全局约定禁止自行安装依赖）
2. **托盘**：采用 §7A（无托盘，关窗即优雅退出）还是 §7B（引入第三方 systray，中等风险）？
3. **端口**：固定 `127.0.0.1:18765` 是否可接受？如需其他端口请指定。
4. **门禁默认**：桌面版 `MIYABI_ACCESS_PASSWORD` 默认留空（无门禁）是否可以？
5. **文档落点**：本文件位于仓库根 `DESKTOP_PLAN.md`，是否需要改名或移到 `docs/`？
