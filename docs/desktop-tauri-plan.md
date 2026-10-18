# Miyabi 桌面端落地方案（Tauri v2 + Go sidecar）

> 分支：`feat/desktop-tauri-v2`
> 状态：M1–M5 已落地并通过 CI 构建；图标已就位。未完成项见 §10 里程碑。
> 目标：把现有 Web 应用变成**原生桌面窗口**，不再依赖外部浏览器，同时尽量**不侵入上游**（`ppxb/miyabi`），以便持续从 `master` 同步。

---

## 1. 目标与约束

**目标**
- 双击启动原生窗口，界面仍是现有 React 应用（走本地 HTTP）。
- 复用全部现有后端（`internal/**`）与前端（`web/**`），前端零改动。
- 单一产物（Windows 优先），跨平台可扩展。

**约束（来自 fork 维护方式）**
- 桌面能力必须是**纯增量**：新增目录，不改上游会动的文件。
- 与上游的唯一契约尽量集中在少数文件，便于上游变动时快速适配。
- 前端自定义（player/搜索/网格等）照常合并，桌面层不增加合并面。

**非目标**
- 不使用系统自带的“打开浏览器”方式（`cmd/portable.go:openBrowser`）。
- 不重写 UI、不替换 Go 后端。

---

## 2. 方案总览

- **壳**：Tauri v2（Rust），窗口使用系统 WebView（Windows = WebView2，macOS = WKWebView，Linux = WebKitGTK）。
- **后端**：现有 Go 程序以 **sidecar**（子进程）方式运行，完全复用 `cmd` 编出的 `miyabi-server`；不再调用 `openBrowser`。
- **前端**：由 Go 内嵌并提供（`embed.go`），Tauri 窗口只是加载 `http://127.0.0.1:<port>`。
- **就绪探测**：TCP 连接 `127.0.0.1:<port>` 直到可连通再显示窗口。上游已删除 `GET /api/health`。
- **局域网发布（可选）**：开关切换 sidecar 的 `MIYABI_LISTEN`（`127.0.0.1:<port>` ↔ `:<port>`），同网段设备即可用浏览器访问（原文仍由 Go 提供），符合“桌面端同时可当局域网服务器”。详见 §6.2。

```
┌──────────────────────────── Tauri 应用 (miyabi desktop) ───────────────────────────┐
│  Rust shell (src-tauri/)                                                            │
│    ├─ 启动 sidecar: miyabi-server.exe  (env: MIYABI_LISTEN / MIYABI_DATA_DIR)              │
│    ├─ TCP 探活 127.0.0.1:<port>                                                     │
│    └─ WebviewWindow(WebviewUrl::External("http://127.0.0.1:<port>"))                │
│                                                                                     │
│  sidecar 进程 (miyabi-server.exe)                                                          │
│    └─ 内嵌 React 前端 + Go API（原样，未修改）                                       │
└─────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. 目录结构（纯增量）

```
src-tauri/                       # 新增：Tauri 壳（纯增量，不动上游）
  Cargo.toml
  build.rs
  tauri.conf.json
  capabilities/default.json      # main/setup 只要 core:default（后端由壳启动）
  .gitignore                     # 忽略 target/ gen/ runtime/
  MODIFICATIONS.md               # 本目录改动记录
  stub/index.html                # frontendDist 占位（真实 UI 由 Go 提供）
  src/
    main.rs                      # 入口 + 插件 + setup
    sidecar.rs                   # 启动/就绪等待/退出（唯一的“契约适配”文件）
    window.rs                    # 创建外部 URL 窗口
  runtime/                       # 构建时放入 miyabi-server.exe（ignored，装进 runtime/）
icon/                            # 应用图标：icon-1024.png（母图）+ miyabi.ico
scripts/
  build-desktop.ps1              # 新增：编前端 + Go 后端 + tauri build
  gen-icon.mjs                   # 旧占位图标生成器，桌面构建已不再使用
.github/workflows/desktop-tauri.yml    # 新增：桌面构建 CI，产出 NSIS 安装包 + 便携 zip
docs/desktop-tauri-plan.md       # 本文
```

`cmd/**`、`internal/**`、`web/**` 保持与上游一致。

---

## 4. 与上游的唯一耦合点

桌面层只依赖以下**稳定接口**（都集中在 `src-tauri/src/sidecar.rs` 一处适配）：

| 契约 | 取值 / 位置 |
|---|---|
| 可执行名 | `runtime/miyabi-server.exe`（与壳分目录，顶层只显示 `Miyabi.exe`；**故意不叫 `miyabi`，避免与壳 `Miyabi.exe` 在 Windows 大小写不敏感下重名**） |
| 监听地址 | 环境变量 `MIYABI_LISTEN`（默认 `:8080`），见 `internal/config/config.go:54` |
| 数据目录 | 环境变量 `MIYABI_DATA_DIR`，见 `internal/config/config.go:55` |
| 便携配置 | exe 同目录 `miyabi.env`，见 `internal/config/portable.go` |
| 就绪判据 | sidecar TCP 连通（上游已删除 `/api/health` 与 `healthcheck` 子命令） |

> 只要上游不改这些，桌面层无需改动；若改动，只改 `sidecar.rs`。

---

## 5. 落地步骤

### 5.1 前置
- Rust 工具链 + Tauri CLI：`cargo install tauri-cli`（或 `pnpm add -D @tauri-apps/cli`）。
- Go 工具链（已有，见 `scripts/dev.bat`）。

### 5.2 构建 Go 后端
- 直接编到 `src-tauri/runtime/miyabi-server.exe`，随 resources 装进 `runtime/`：
  ```powershell
  New-Item -ItemType Directory -Force -Path src-tauri/runtime | Out-Null
  go build -trimpath -ldflags "-s -w -H=windowsgui" -o "src-tauri/runtime/miyabi-server.exe" ./cmd
  ```
- 不用 `externalBin`：sidecar 会被安装器放到壳旁边，顶层就会出现两个 exe。resources 保留 `runtime/` 这一层。
- 说明：Go 端**不需要改动**；`-H=windowsgui` 由构建脚本决定，不动源码。

### 5.3 Tauri 壳配置
- `tauri.conf.json`：
  ```jsonc
  {
    "productName": "Miyabi",
    "identifier": "com.ppxb.miyabi.desktop",
    "bundle": {
      "resources": { "runtime/": "runtime/" },
      "windows": { "webviewInstallMode": { "type": "embedBootstrapper" } }
    },
    "build": { "frontendDist": "stub" }   // UI 由 Go 提供，仅需最小占位
  }
  ```
- `capabilities/default.json` 只要 `core:default`；后端由壳用普通子进程启动，webview 不需要 shell 权限。
- 主窗口**不**写进配置的 `app.windows`，改在 Rust `setup` 里按就绪结果动态创建（见下）。

### 5.4 启动 / 就绪 / 退出（`src-tauri/src/main.rs` + `sidecar.rs`）
1. `setup`：
   - 选端口（见 §6），以普通子进程启动 `runtime/miyabi-server.exe`（`std::process::Command`），注入 `MIYABI_LISTEN`、`MIYABI_DATA_DIR`，spawn。
   - TCP 连接 `127.0.0.1:<port>`，带超时（如 30s）；失败弹原生错误并退出。上游已删除 `/api/health`。
   - 通过后 `WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url))` 建窗。
2. 退出：监听 `RunEvent::Exit` → kill 并 wait 后端（Windows 建议用 Job Object 兜底，避免残留）。
3. 单实例：`tauri-plugin-single-instance`。二次启动时聚焦既有窗口，而不是再起后端。

> 直接复用上游已有的二次启动语义：`cmd/cmd.go` 在便携模式且端口已被占用时直接打开浏览器——桌面端把“打开浏览器”换成“连接到既有实例并聚焦窗口”。注入 `MIYABI_LISTEN` / `MIYABI_DATA_DIR` 后 Go 端退出便携模式，因此后端不会再拉起浏览器。

### 5.5 WebView2
- `webviewInstallMode`: `embedBootstrapper`（离线引导安装）或 `fixedRuntime`（完全自带，避免依赖系统是否安装）；开发阶段可 `downloadBootstrapper`。

### 5.6 构建与运行
- **安装包**：`tauri build` → Windows **NSIS 安装包**（`runtime/` 作为资源一并打入）。
- **便携版**：不安装，直接跑的 zip = `Miyabi.exe` + `runtime/`（`miyabi-server.exe` 与 `portable` 标记）+ `LICENSE` + `使用说明.md`。顶层只有一个 exe。
- 本地一键：`scripts/build-desktop.ps1`（本机需 Rust）；或交给 CI。
- dev 预览需本机 Rust：先 `pnpm --dir web build`（内嵌前端），再编 `runtime/miyabi-server.exe`，最后 `pnpm dlx @tauri-apps/cli@^2 dev`。本项目不做本地调试，故不提供 dev 脚本。

### 5.7 CI（新增，不动上游）
`.github/workflows/desktop-tauri.yml`（名称 `Build desktop (Tauri)`，与 `Build Windows portable`、`Publish Miyabi image` 明显区分）：
- 触发：`workflow_dispatch`（带 `version` 输入）、`feat/desktop-*` 分支推送、`v*.*.*` tag。
- 步骤：装 Go/Rust/Node → 建前端（`web build`，供 `go:embed`）→ 建 `runtime/miyabi-server.exe` → `tauri build`（图标用仓库根的 `icon/miyabi.ico`）。
- 产物：`miyabi-<version>-windows-x86_64-tauri-setup.exe`（NSIS 安装包）+ `miyabi-<version>-windows-x86_64-tauri-portable.zip`（便携），**各自独立**。上传用 `archive: false`，下载名就是文件名。
- 上传：分成两个 artifact（`...-tauri-setup.exe` / `...-tauri-portable.zip`），都用 `archive: false`，下载名就是文件名，不会再多一层 zip。

---

## 6. 端口、监听与局域网发布

### 6.1 端口与数据目录
- **端口**：局域网关闭时绑定 `127.0.0.1:0` 取空闲端口。局域网开启时使用设置里的端口（首次默认 `8080`），先探测占用再传给 sidecar。
- **数据目录**：
  - 安装版：`%APPDATA%\com.ppxb.miyabi.desktop`（通过 `MIYABI_DATA_DIR` 注入）。
  - 便携版：壳同级 `data\`。用 `runtime/portable` 空文件区分（安装版没有），不看后端是否同目录。
- **桌面配置**：`desktop.json`。安装版在 AppData；便携版在 `runtime/` 里，不放进 `data\`。首次启动由本地向导写入，之后局域网端口和访问密码在设置页修改，保存时重启后端。

### 6.2 局域网访问（LAN 发布）
桌面端提供「局域网访问」开关（默认**关**）。它不引入任何新后端能力，只是改变 sidecar 的监听地址：

| 模式 | `MIYABI_LISTEN` | 谁能访问 |
|---|---|---|
| 仅本机（默认） | `127.0.0.1:<port>` | 只有本机的桌面窗口 |
| 局域网 | `:<port>`（即 `0.0.0.0`，绑定全部网卡） | 同网段设备可访问 |

- **本地窗口**始终连 `http://127.0.0.1:<port>`（`app.LocalURL` 会把通配监听折算成回环地址，见 `internal/app/app.go:312`），不受开关影响。
- **切换需重启 sidecar**：改变绑定地址必须重新拉起 Go 进程；壳在开关切换时优雅停掉旧 sidecar、用新的 `MIYABI_LISTEN` 再起一次，健康通过后沿用同一窗口（重新加载即回到 `127.0.0.1`）。
- **展示局域网地址**：壳枚举本机各网卡 IPv4，拼成 `http://<ip>:<port>`（可多张网卡）展示/复制。为**不改上游前端**，优先通过壳层呈现：系统托盘菜单「复制局域网地址」+ 首次开启时一个原生提示；后续若需要也可做一处极小的前端增量（另算合并成本）。
- **安全（重要）**：开启 LAN 时**必须**设置访问密码，通过 `MIYABI_ACCESS_PASSWORD` 注入（上游已支持，见 `internal/config/config.go:90` 与访问门），否则同网段任何人可无认证访问。壳应在开启开关时强制/引导设置密码。
- **防火墙**：Windows 首次监听 `0.0.0.0` 会弹出放行提示；也可由安装包/脚本预置一条入站规则（仅限对应端口）。安装版首启时提示用户放行「专用网络」。
- **反向代理/可信代理**（可选，进阶）：如前面挂了反代，可复用上游 `MIYABI_TRUSTED_PROXIES`、`MIYABI_PUBLIC_URL`，桌面层无需改动。

> 说明：以上全部基于**现有** `MIYABI_LISTEN` / `MIYABI_ACCESS_PASSWORD` / `MIYABI_TRUSTED_PROXIES`，因此不新增与上游的耦合面（仍集中在 `sidecar.rs`）。

---

## 7. 生命周期与单实例

- 顺序：起 sidecar → TCP 探活 → 开窗。
- 关闭：关窗/退出 → 结束 sidecar；用 Windows Job Object 兜底进程回收。
- 二次启动：探测既有实例 → 复用/激活，不起新进程。

---

## 8. 风险与缓解

| 风险 | 缓解 |
|---|---|
| WebView2 未安装 | `embedBootstrapper` / `fixedRuntime` |
| 端口被占用 | 动态选端口 + env 传递 |
| sidecar 残留进程 | 退出钩子 + Job Object |
| 上游接口变动（env / health / 子命令） | 契约集中在 `sidecar.rs` 一处 |
| 杀软误报 / 无签名 | 后续代码签名；sidecar 与壳同目录 |
| 局域网暴露且未设密码 | 开启 LAN 时强制设置 `MIYABI_ACCESS_PASSWORD` |
| 防火墙拦截 LAN 访问 | 首启引导放行「专用网络」/ 预置对应端口入站规则 |
| 切换 LAN 开关的服务中断 | 重启 sidecar 期间顶部提示“正在重启服务”，就绪后重载窗口 |
| 前端自定义合并冲突 | 与本方案无关（`web/**` 照常合并） |

---

## 9. 与上游同步流程

1. `git fetch upstream`（需先把 `ppxb/miyabi` 加为 `upstream` 远端）。
2. `git merge upstream/master`（或 rebase）到桌面分支；冲突主要来自 `web/**` 自定义，`src-tauri/**` 不受影响。
3. 重新构建 sidecar 即可，**壳通常无需改动**。

```powershell
git remote add upstream git@github.com:ppxb/miyabi.git   # 一次性
git fetch upstream
git merge upstream/master
```

---

## 10. 里程碑（待办）

- [x] M1 骨架：`src-tauri/` 建窗口并指向本地服务，已通过 CI 构建。
- [x] M2 sidecar：自动起/停 + 就绪等待 + 单实例复用，已通过 CI 构建。
- [x] M3 打包：Windows NSIS 安装包，Go 后端以 resources 装入 `runtime/`。
- [x] M4 局域网：设置页开关 + 端口 + 访问密码 + 局域网地址复制。**未做**：Windows 防火墙引导。
- [x] M5 CI：`desktop-tauri.yml` 出安装包与便携 zip（`archive: false`）；WebView2 用 `embedBootstrapper`。
- [x] M6 图标：`icon/miyabi.ico`（深色圆角底板，16–256 多尺寸）已接入 `bundle.icon`。
- [x] M6 图标小尺寸重做：16–1024 全部改为深色圆角底板（`#1b2233`）+ 插画占画布 72%，各尺寸可见比例稳定在 57% 左右；`miyabi.ico` 由 `scripts/build-ico.py` 重新生成（16–128 为 BMP 条目，256 为 PNG）。
- [x] 图标格式修复：ICO 之前全部用 PNG 压缩，Windows 只认 256 那一档用 PNG，小尺寸必须是 BMP，否则资源管理器列表视图空白。
- [ ] M6 打磨：自动更新（Tauri updater）、代码签名。

---

## 附录：关键接口清单

- 启动：`miyabi`（无参即起 HTTP 服务；上游已拒绝 `healthcheck` 等子命令）。
- 环境变量：`MIYABI_LISTEN`、`MIYABI_DATA_DIR`（其余 Emby/STRM/密码等按需）。
- 便携配置：exe 同目录 `miyabi.env`。
- 就绪：TCP 连通即可。上游已删除 `GET /api/health`。
- 前端：由 `embed.go` 内嵌，`app.LocalURL(listen)` 得到回环地址。
