@echo off
:: Quick build script - downloads dependencies and builds

cd /d "%~dp0"

echo Downloading dependencies...
go mod download
if errorlevel 1 (
    echo Failed to download dependencies!
    echo Make sure you have Go installed: https://go.dev/dl/
    pause
    exit /b 1
)

echo.
echo Building ollama-proxy.exe...
go build -ldflags="-w -s" -o ollama-proxy.exe .

if exist ollama-proxy.exe (
    echo.
    echo SUCCESS! Built ollama-proxy.exe
    echo.
    echo Run with: ollama-proxy.exe serve
) else (
    echo.
    echo BUILD FAILED!
    pause
    exit /b 1
)