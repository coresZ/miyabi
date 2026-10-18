@echo off
setlocal
chcp 65001 >nul

rem Launches the portable Miyabi build next to this script. miyabi.exe is
rem window-less and opens its own browser once the server is ready, so this
rem only starts it in the background and returns.

if not exist "%~dp0miyabi.exe" (
    echo 未找到 miyabi.exe，请将本脚本放在 Miyabi 程序同级目录。
    pause
    exit /b 1
)

start "" "%~dp0miyabi.exe"
exit /b 0
