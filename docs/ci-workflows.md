# Miyabi GitHub Actions 工作流说明

本文说明仓库内 CI 的入口、职责与推荐用法。工作流文件在 [`.github/workflows/`](../.github/workflows/)。

## 总览

| 类型 | 名称 | 触发方式 | 作用 |
|------|------|----------|------|
| **入口** | Manual Build | Actions 手动运行 | 按需构建，只上传 Artifact，**不**打 tag、**不**发 Release |
| **入口** | Release | Actions 手动运行，或推送 `v*` tag | **正式发版**：打 tag、创建 GitHub Release、上传产物 |
| 可复用 | Build desktop (Tauri) | 仅被上面两个调用 | Windows 桌面安装包 + 桌面便携 zip |
| 可复用 | Build Windows portable | 仅被上面两个调用 | 服务端/浏览器便携包（无 Tauri 窗口） |
| 可复用 | Publish Miyabi image | 仅被上面两个调用 | Docker 镜像（可选择是否 push 到 GHCR） |

> 可复用工作流**没有**独立的「日常入口」，也不会在推送到 `feat/desktop-*` 时自动跑，避免无关构建刷屏。

---

## Manual Build（验证构建）

**路径**：GitHub → Actions → **Manual Build** → Run workflow

### 参数

| 参数 | 说明 | 默认 |
|------|------|------|
| 分支（Use workflow from） | 用哪支代码的最新提交 | 在界面选择 |
| **desktop** | 构建 Windows 桌面（Tauri 安装包 + 便携 zip） | 开启 |
| **portable** | 构建服务端/浏览器便携包 | 开启 |
| **docker** | 构建 Docker 镜像（**不推送**，只暖缓存） | 关闭 |
| **version** | 产物命名标签；留空则为 `分支名-短sha` | 空 |

### 结果

- 产物出现在该次 run 的 **Artifacts**
- 不会创建 git tag
- 不会创建或修改 GitHub Release

### 适用场景

- 改完桌面/便携包逻辑，想先装包验证
- 指定功能分支（如 `feat/desktop-tauri-v3`）出测试包

---

## Release（正式发版）

**路径**：GitHub → Actions → **Release** → Run workflow  

或本地：

```bash
git tag v0.4.20
git push origin v0.4.20
```

（推送 `v*` tag 会按 **全量** 构建：桌面 + 便携包 + Docker。）

### 手动运行时的参数

| 参数 | 说明 |
|------|------|
| 分支 | 发版用的源码分支（例如含桌面的 `feat/desktop-tauri-v3`） |
| **version**（必填） | 版本号，如 `0.4.20`（可带或不带前缀 `v`）→ 创建 tag `v0.4.20` |
| **desktop** | 是否构建并上传桌面产物 |
| **portable** | 是否构建并上传服务端便携包 |
| **docker** | 是否构建并 **push** Docker 镜像 |
| **prerelease** | 是否标记为 pre-release |

### 流程简述

1. 在所选分支 HEAD 上创建（或校验）tag `v{version}`  
2. 创建 **draft** Release  
3. 按勾选调用可复用工作流构建并上传资产  
4. 全部成功后将 Release **取消 draft**（公开发布）

### 适用场景

- 正式版本对外发布  
- 需要 GitHub Release 页面与可下载安装包  

> 若只想出包、不要 tag/Release，请用 **Manual Build**，不要留空 version 跑 Release（当前 Release 要求必须填版本号）。

---

## 可复用工作流（实现细节）

### `desktop-tauri.yml`

- **产物**：NSIS 安装包 + 桌面便携 zip（`miyabi.exe` + `runtime/` 内嵌服务）  
- **环境**：`windows-latest`  
- **注意**：检出树必须包含 `src-tauri/`（完整 Tauri 工程）。仅有后端、没有桌面目录的分支会失败。

### `windows-portable.yml`

- **产物**：服务端/浏览器便携 zip（运行 HTTP 服务，无桌面壳）  
- **环境**：多架构矩阵（如 amd64 / arm64）  
- 带基础冒烟：拉起进程并探测本地 API/页面  

### `docker-publish.yml`

- **产物**：多架构镜像（如 linux/amd64、linux/arm64）  
- **Manual Build**：`push=false`，只构建暖缓存  
- **Release**：`push=true`，推送到配置的 GHCR 镜像名  

---

## 推荐用法速查

1. **日常验证桌面包**  
   Manual Build → 分支选 `feat/desktop-tauri-v3`（或当前桌面开发分支）→ 勾选 desktop（按需 portable）→ 下载 Artifact。

2. **正式发版**  
   Release → 同一桌面分支 → version 填 `0.4.20` → 勾选需要的组件 → 在 Releases 页查看结果。

3. **用 tag 发版**  
   ```bash
   git checkout <发版分支>
   git tag v0.4.20
   git push origin v0.4.20
   ```

---

## 权限说明

- Manual Build / Release 在调用方可复用工作流时需要 `contents: write`（即使 Manual Build 不上传 Release，GitHub 仍会在解析阶段校验被调工作流的权限声明）。  
- Manual Build **不会**创建 tag 或 Release，仅上传 workflow Artifact。

---

## 相关文件

| 路径 | 说明 |
|------|------|
| `.github/workflows/build.yml` | Manual Build |
| `.github/workflows/release.yml` | Release |
| `.github/workflows/desktop-tauri.yml` | 桌面可复用 |
| `.github/workflows/windows-portable.yml` | 便携包可复用 |
| `.github/workflows/docker-publish.yml` | 镜像可复用 |
