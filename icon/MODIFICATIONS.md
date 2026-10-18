# icon MODIFICATIONS

用途：桌面端应用图标。

## 目录内容

- `artwork-1024.png`：原始透明插画，只在重做底板时需要。
- `icon-1024.png`：成品母图（深色圆角底板 + 插画）。所有尺寸和 `miyabi.ico` 都从它生成。
- `miyabi.ico`：Windows 图标，`src-tauri/tauri.conf.json` 的 `bundle.icon` 指向它。

其余尺寸的 PNG 已删除，都能由母图重新生成。

## 设计

- 深色圆角底板（`#1b2233`，圆角半径 22%），插画居中、占画布 72%（可见图形约 57%）。
- 不用白色底板：插画本身以白色为主，放白底上扇形和路径会消失，只剩深色三角和粉色花瓣。

## 生成

```powershell
python scripts/build-ico.py icon/icon-1024.png icon/miyabi.ico
```

- **16–128 必须写成 BMP(DIB) 条目，256 才能用 PNG。** Windows 资源管理器在「列表 / 小图标」视图只认 BMP 小尺寸；全 PNG 的 ICO 会显示空白。
- 插画按目标占比显式 `resize`，不能用 `thumbnail`：它只缩小不放大，源图不足时大尺寸反而会让图形占比变小。
