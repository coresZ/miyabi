# Miyabi 桌面版 使用说明

- **启动**：双击 **`Miyabi.exe`** —— 打开原生窗口，无需浏览器。
- 第一次启动会问访问密码和是否开放局域网。115 登录、媒体目录仍在设置里。
- **`runtime\miyabi-server.exe` 是内部后端**，由 `Miyabi.exe` 自动拉起；不要单独运行它。
- 关闭窗口会一并结束后端进程。
- 局域网开关和端口在设置页的「桌面」里。保存后服务会重启。开启局域网时必须设置访问密码；Windows 可能会询问防火墙权限。

## 目录

```
Miyabi.exe
使用说明.md
LICENSE
runtime\
  miyabi-server.exe      后端，不要单独运行
  portable               便携版标记（安装版没有）
  desktop.json           便携版的首次配置
data\                    首次运行后出现：数据库、封面、日志
```

`Miyabi.exe` 和 `runtime\` 必须放在一起，不要只拷走 exe。

## 安装版

- `runtime\` 里没有 `portable`。
- 第一次启动可以选择数据目录，不选则用 `%APPDATA%\com.ppxb.miyabi.desktop`。
- 桌面配置：同一目录下的 `desktop.json`。

## 便携版

- `runtime\` 里有一个空的 `portable` 文件。不要删，否则会被当成安装版，数据改写到 AppData。
- 数据目录固定是旁边的 `data\`，第一次启动不会再问。
- 整个文件夹可以拷走。
