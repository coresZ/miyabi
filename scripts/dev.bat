@echo off
setlocal
chcp 65001 >nul
cd /d "%~dp0.."

rem Development mode: the backend runs with -tags dev (no embedded frontend)
rem and Vite serves the UI on :5173 with /api proxied to :8080.

where go >nul 2>&1
if errorlevel 1 goto :nogo
where pnpm >nul 2>&1
if errorlevel 1 goto :nopnpm

echo 启动后端（:8080）与前端热重载（:5173，局域网可访问）...
start "miyabi backend" cmd /k "go run -tags dev ./cmd"
start "miyabi frontend" cmd /k "pnpm --dir web dev --host"
timeout /t 3 /nobreak >nul
start "" "http://127.0.0.1:5173"
echo 局域网设备请用前端窗口里打印的 Network 地址访问（首次需放行 Windows 防火墙）。
echo 关闭弹出的后端/前端窗口即可停止。
exit /b 0

:nogo
echo 未找到 go，请先安装 Go。
pause
exit /b 1

:nopnpm
echo 未找到 pnpm，请先安装 Node.js 与 pnpm。
pause
exit /b 1
