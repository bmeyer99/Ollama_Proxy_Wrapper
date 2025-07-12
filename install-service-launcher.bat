@echo off
REM Launcher for Install-Service.ps1 that bypasses execution policy

echo Starting Ollama Metrics Proxy Service Installer...
echo.

REM Check if running as admin
net session >nul 2>&1
if %errorlevel% == 0 (
    REM Already admin, just run it
    powershell.exe -ExecutionPolicy Bypass -File "%~dp0Install-Service.ps1"
) else (
    REM Need to elevate - this will prompt for admin
    echo This installer requires Administrator privileges.
    echo You will be prompted for permission...
    echo.
    powershell.exe -Command "Start-Process powershell.exe -ArgumentList '-ExecutionPolicy Bypass -File \"%~dp0Install-Service.ps1\"' -Verb RunAs"
)