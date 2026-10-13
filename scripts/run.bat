@echo off
setlocal
chcp 65001 >nul
cd /d "%~dp0.."

rem Runs Miyabi from source with the frontend embedded, like a release build:
rem build web/dist once when missing, then start the backend on :8080.

where go >nul 2>&1
if errorlevel 1 goto :nogo

if exist "web\dist\index.html" goto :run

where pnpm >nul 2>&1
if errorlevel 1 goto :nopnpm

echo 首次运行，构建前端 web/dist ...
pushd web
call pnpm install --frozen-lockfile
if errorlevel 1 goto :buildfail
call pnpm build
if errorlevel 1 goto :buildfail
popd

:run
echo 启动 Miyabi（源码，http://127.0.0.1:8080），按 Ctrl+C 停止。
go run ./cmd
exit /b %errorlevel%

:nogo
echo 未找到 go，请先安装 Go。
pause
exit /b 1

:nopnpm
echo 未找到 pnpm，请先安装 Node.js 与 pnpm。
pause
exit /b 1

:buildfail
popd
echo 前端构建失败。
pause
exit /b 1
