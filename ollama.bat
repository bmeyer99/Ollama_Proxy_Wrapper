@echo off
:: Ollama Metrics Proxy Launcher
:: This replaces the standard ollama command with the metrics proxy version

:: Get the directory where this script is located
set SCRIPT_DIR=%~dp0

:: Check if proxy executable exists
if not exist "%SCRIPT_DIR%ollama-proxy.exe" (
    echo ERROR: ollama-proxy.exe not found!
    echo Please run quick-build.bat first.
    exit /b 1
)

:: Run the proxy with all arguments passed through
"%SCRIPT_DIR%ollama-proxy.exe" %*