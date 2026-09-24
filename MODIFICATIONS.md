# MODIFICATIONS.md

本文件记录仓库根目录范围内的修改。新修改请追加到最上方。

## 2026-09-24 — 桌面版（Wails）代码落地（纯代码，CI 构建）

- 功能：新增 Wails 桌面壳。Wails 管窗口 / 单实例 / 生命周期，WebView 经 302 直连本机 Gin，前端 / API / SSE 同源；控制台与 Docker 不受影响。
- 修改内容：
  - `internal/config`：新增 `RuntimeMode`（默认 `RuntimeServer`）与 `Config.Mode`。
  - `internal/desktop`（新增）：exe 目录、默认 data 目录、固定端口 `127.0.0.1:18765`、`LocalURL`。
  - `internal/app`：`New` 支持 `WithDesktop` 选项；抽出 `RunListener(ctx, net.Listener)`，`Run` 内部改用它。
  - `internal/api`：新增可选 `DesktopHooks` 与 `/api/desktop/reveal-data`、`/api/desktop/quit`（仅桌面模式注册）。
  - `cmd/miyabi-desktop`（新增）：`main.go`（`windows && miyabidesktop`）与 `main_stub.go`（其余情况，保证 `./...` 可编译测试）。
  - `wails.json`（新增）；`.gitignore` 忽略 `/build/` 与 `miyabi-desktop.exe`。
  - CI：`release-binaries.yml` 新增 `build-desktop`（windows-latest，现场 `go get` wails + `wails build -tags miyabidesktop`），`release` 改为 `needs: [build, build-desktop]`。
  - `DESKTOP_PLAN.md`：§10/§11 更新为「纯代码 + CI 构建」。
- 涉及文件与位置：
  - `internal/config/mode.go`（新增）、`internal/config/config.go`（`Config.Mode` + 默认值）
  - `internal/desktop/paths.go`（新增）
  - `internal/app/app.go`（`Option` / `WithDesktop` / `RunListener`）
  - `internal/api/desktop.go`（新增）、`internal/api/router.go`（`Dependencies.Desktop` + 路由注册）
  - `cmd/miyabi-desktop/main.go`、`cmd/miyabi-desktop/main_stub.go`（新增）
  - `wails.json`（新增）、`.gitignore`、`.github/workflows/release-binaries.yml`
- 验证基线：本地 `go build -tags dev ./...` 与 `go test -tags dev ./...` 全绿（未使用 `web/dist`，未拉取 Wails）。桌面真实入口因缺 Wails 模块未在本地编译，待 CI（windows-latest）验证。
- 备注：仓库 `go.mod` 不提交 wails 依赖，由 CI 现场解析；`cmd/miyabi-desktop` 的编辑器诊断报错属预期。

## 2026-09-24 — 桌面版（Wails）一步到位方案文档

- 功能：Miyabi 桌面版（Wails）落地可行性分析与方案文档 v1.0。
- 修改内容：新增方案文档，含可行性核验证据、相对原文的修订点、待确认决策。
- 涉及文件与位置：
  - 新增 `DESKTOP_PLAN.md`（仓库根）。
  - 新增 `MODIFICATIONS.md`（本文件，仓库根）。
- 备注：本次为纯文档改动，未改代码/构建/配置，未递增版本号。方案中的代码改动尚未实施。
