# icon MODIFICATIONS

用途：桌面端应用图标（Tauri `bundle.icon`）。

## 来源
从 `miyabi_icon/` 的原始素材里挑出**白色透明底**那一组（`miyabi_transparent_*`）复制而来；其余深色底版本未采用。

## 文件
- `icon-16.png` … `icon-1024.png`：按尺寸拆分的 PNG，白色图形 + 透明背景。
- `miyabi.ico`：由 `icon-1024.png` 生成，含 16/24/32/48/64/128/256 七个尺寸，供 Windows 安装包与 exe 使用。

## 接入
- `src-tauri/tauri.conf.json` 的 `bundle.icon = ["../icon/miyabi.ico"]`。
- 构建不再运行 `scripts/gen-icon.mjs` + `tauri icon`；图标直接来自本目录。
