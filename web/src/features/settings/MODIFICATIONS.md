# web/src/features/settings MODIFICATIONS

用途：设置页各分区。

## 文件
- `desktop-bridge.ts`：桌面壳探测。没有 `window.__TAURI__` 时设置页不渲染桌面分区，浏览器里的同一页面不受影响。
- `desktop-section.tsx`：桌面分区。数据目录只读；局域网开关、端口、访问密码。保存调用 `apply_desktop`，由壳重启 sidecar。只在桌面窗口出现，分隔线也由本组件自己渲染。
