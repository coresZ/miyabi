@echo off
setlocal
chcp 65001 >nul
title Miyabi Desktop (debug)
cd /d "%~dp0.."

where powershell.exe >nul 2>&1
if errorlevel 1 (
    echo PowerShell not found.
    pause
    exit /b 1
)

powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0run-desktop.ps1" %*
set "CODE=%ERRORLEVEL%"
if not "%CODE%"=="0" (
    echo.
    echo Build or launch failed with exit code %CODE%.
    pause
)
exit /b %CODE%
