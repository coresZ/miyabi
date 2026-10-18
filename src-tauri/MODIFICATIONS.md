# src-tauri MODIFICATIONS

用途：Miyabi 桌面端外壳（Tauri v2 + Go sidecar）。纯增量目录，不改动上游文件。

## 架构
- Rust 壳启动 Go sidecar（复用 `cmd` 编出的 `miyabi-server`），轮询就绪后开一个加载 `http://127.0.0.1:<port>` 的原生窗口。
- 前端由 Go 端 `embed.go` 内嵌提供，桌面层不参与前端构建。

## 文件
- `Cargo.toml` / `build.rs`：Tauri v2 依赖与构建脚本。
- `tauri.conf.json`：`bundle.externalBin = ["binaries/miyabi-server"]`（sidecar 取名 `miyabi-server`，避免与壳 `Miyabi.exe` 在 Windows 大小写不敏感下重名覆盖）、Windows `webviewInstallMode=embedBootstrapper`、`frontendDist=stub`（真实 UI 由 Go 提供）。
- `capabilities/default.json`：给 `main` 窗口 `shell:allow-execute`（sidecar 作用域）。
- `src/main.rs`：Builder + 插件 + setup 启 sidecar/开窗 + 退出回收。
- `src/sidecar.rs`：**唯一的上游契约适配点**——选空闲端口、起 sidecar（`MIYABI_LISTEN`/`MIYABI_DATA_DIR`）、TCP 就绪等待、退出 kill。
- `src/window.rs`：`WebviewWindowBuilder` + `WebviewUrl::External` 创建窗口。
- `stub/index.html`：满足 `frontendDist` 的占位。
- `.gitignore`：忽略 `target/`、`gen/`、`binaries/`、`icons/`、`app-icon.png`。

## 与上游耦合点（集中在 `src/sidecar.rs`）
- 可执行名 `miyabi-server`，构建入口 `./cmd`（上游已把 `cmd/miyabi` 扁平化到 `cmd`）；环境变量 `MIYABI_LISTEN` / `MIYABI_DATA_DIR`（设置任一即令 Go 端退出“便携模式”，从而**不会拉起浏览器**）；就绪判据为 TCP 连通。上游已删除 `/api/health` 与 `healthcheck` 子命令，不要再升级成 HTTP 探活。

## 首次配置
- `stub/setup.html`：sidecar 启动前的本地向导。问数据目录、访问密码、局域网开关（默认关，开则必须有密码，端口默认 8080）。
- `src/prefs.rs`：`desktop.json`。exe 旁边有空文件 `portable` 才是便携版（安装版写 AppData，便携版写 exe 旁边）。
- `src/commands.rs`：`desktop_defaults` / `complete_setup` / `desktop_config` / `apply_desktop`。设置页改局域网端口后重启 sidecar 并重载主窗口。
- `capabilities/desktop-remote.json`：只允许 `http://127.0.0.1:*/*` 和 `http://localhost:*/*` 调这四个命令。
- `build.rs`：用 `AppManifest::commands` 生成 `allow-desktop-defaults` 等权限。不声明的话 capability 引用会在构建期直接失败。
- 命令返回 `Result<(), String>` 时，`tauri::Error` 不能直接 `?`，要 `map_err`。`setup` 里的 `String` 错误同样要转成 `std::error::Error`。
- 便携 zip 组装前先确认 `target/release/miyabi-desktop.exe` 和 NSIS 都已产出。release 二进制沿用 Cargo 包名，不是 `productName`。

## 状态
- M1/M2 骨架：代码已落，**待 CI（GitHub Actions）首次构建验证**（本地未装 Rust）。
- 未做：局域网开关（M4）、图标正式化（M6）、Job Object 兜底回收。

## 验证
- CI：`.github/workflows/desktop-tauri.yml`（`Build desktop (Tauri)`，Windows runner：装 Go/Rust/Node → 建前端 → 建 sidecar → `tauri build` → 产出 NSIS 安装包与便携 zip）。
- 本地未装 Rust，故不在本地构建。
