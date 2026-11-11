# Miyabi CI 工作流

## 入口（在 Actions 页点 Run workflow）

| 工作流 | 用途 | 是否发版 |
|--------|------|----------|
| **Manual Build** | 选分支、勾选组件，只出 Artifact | 否 |
| **Release** | 填版本号、勾选组件；或推送 `v*` tag | 是（tag + GitHub Release） |

## 可复用（不单独展示为日常入口）

| 文件 | 产物 |
|------|------|
| `desktop-tauri.yml` | Windows 桌面安装包 + 桌面便携 zip |
| `windows-portable.yml` | 服务端/浏览器便携包 |
| `docker-publish.yml` | Docker 镜像 |

## 推荐用法

1. 日常验证：`Manual Build` → 选 `feat/desktop-tauri-v3` 等分支 → 勾选 desktop / portable  
2. 正式发版：`Release` → 同一分支 → 版本如 `0.4.20` → 勾选需要的组件  
3. 或本地：`git tag v0.4.20 && git push origin v0.4.20`（全量桌面+便携包+Docker）

桌面构建需要含 `src-tauri/` 的分支；纯 `master` 若没有桌面工程会失败。
