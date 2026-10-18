# src-tauri MODIFICATIONS

用途：Miyabi 桌面端外壳（Tauri v2 + Go sidecar）。纯增量目录，不改动上游文件。

## 架构
- Rust 壳启动 Go sidecar（复用 `cmd` 编出的 `miyabi-server`），轮询就绪后开一个加载 `http://127.0.0.1:<port>` 的原生窗口。
- 前端由 Go 端 `embed.go` 内嵌提供，桌面层不参与前端构建。

## 文件
- `Cargo.toml` / `build.rs`：Tauri v2 依赖与构建脚本。
- `tauri.conf.json`：`bundle.icon = ["../icon/miyabi.ico"]`（仓库根的 `icon/`，白色透明底，已提交）、`bundle.resources = { "runtime/": "runtime/" }`（Go 后端作为资源装进 `runtime/`，不再用 `externalBin`，否则安装器会把第二个 exe 摊在壳旁边）、Windows `webviewInstallMode=embedBootstrapper`、`frontendDist=stub`（真实 UI 由 Go 提供）。
- `capabilities/default.json`：`main` 和 `setup` 只要 `core:default`。后端由壳启动，webview 不需要 shell 权限。
- `src/main.rs`：Builder + 插件 + setup 启后端/开窗 + 退出回收。
- `src/sidecar.rs`：**唯一的上游契约适配点**——选空闲端口、起 `runtime/miyabi-server.exe`（`MIYABI_LISTEN`/`MIYABI_DATA_DIR`）、TCP 就绪等待、退出 kill。
- `src/window.rs`：`WebviewWindowBuilder` + `WebviewUrl::External` 创建窗口。
- `stub/index.html`：满足 `frontendDist` 的占位。
- `.gitignore`：忽略 `target/`、`gen/`、`runtime/`。图标已提交在仓库根的 `icon/`。

## 与上游耦合点（集中在 `src/sidecar.rs`）
- 可执行名 `miyabi-server`，放在 `runtime/`，构建入口 `./cmd`（上游已把 `cmd/miyabi` 扁平化到 `cmd`）；环境变量 `MIYABI_LISTEN` / `MIYABI_DATA_DIR`（设置任一即令 Go 端退出“便携模式”，从而**不会拉起浏览器**）；就绪判据为 TCP 连通。上游已删除 `/api/health` 与 `healthcheck` 子命令，不要再升级成 HTTP 探活。

## 首次配置
- `stub/setup.html`：sidecar 启动前的本地向导。便携版不问数据目录，固定 exe 旁边的 `data\`。安装版用系统文件夹对话框，默认 AppData，路径只读。访问密码可选；局域网默认关，开启必须有密码，端口默认 8080。
- `src/prefs.rs`：`desktop.json`。`runtime/` 里有空文件 `portable` 才是便携版（安装版写 AppData，便携版写 `runtime/`）。数据目录在便携版固定为壳旁边的 `data/`。
- `src/commands.rs`：`desktop_defaults` / `pick_data_dir` / `complete_setup` / `desktop_config` / `apply_desktop`。设置页改局域网端口后重启后端并重载主窗口。
- `capabilities/desktop-setup.json`：本地 `setup` 窗口才能调 `desktop_defaults` / `pick_data_dir` / `complete_setup`。远程页没有这些权限。
- `capabilities/desktop-remote.json`：只允许 `http://127.0.0.1:*/*` 和 `http://localhost:*/*` 调 `desktop_config` / `apply_desktop`。
- 产物文件名：`miyabi-<version>-windows-x86_64-tauri-portable.zip` 与 `...-tauri-setup.exe`。上传用 `archive: false`，避免 GitHub 再套一层 zip。
- `build.rs`：用 `AppManifest::commands` 生成 `allow-desktop-defaults` 等权限。不声明的话 capability 引用会在构建期直接失败。
- 命令返回 `Result<(), String>` 时，`tauri::Error` 不能直接 `?`，要 `map_err`。`setup` 里的 `String` 错误同样要转成 `std::error::Error`。
- 便携 zip 组装前先确认 `target/release/miyabi-desktop.exe` 和 NSIS 都已产出。release 二进制沿用 Cargo 包名，不是 `productName`。zip 顶层只有 `Miyabi.exe`、`runtime/`、说明和 LICENSE；`portable` 标记写在 `runtime/` 里。

## 状态
- M1/M2 骨架：代码已落，**待 CI（GitHub Actions）首次构建验证**（本地未装 Rust）。
- 未做：局域网开关（M4）、图标正式化（M6）、Job Object 兜底回收。

## 验证
- CI：`.github/workflows/desktop-tauri.yml`（`Build desktop (Tauri)`，Windows runner：装 Go/Rust/Node → 建前端 → 建 `runtime/miyabi-server.exe` → `tauri icon` → `tauri build` → 产出 NSIS 安装包与便携 zip）。
- 本地未装 Rust，故不在本地构建。
