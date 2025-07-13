@echo off
echo Building Ollama Proxy for Windows...

:: Clean up old builds
if exist ollama-proxy.exe del /f ollama-proxy.exe

:: Download dependencies
echo Downloading dependencies...
go mod download

:: Build the executable
echo Building executable...
go build -ldflags="-w -s" -o ollama-proxy.exe .

if errorlevel 1 (
    echo Build failed!
    exit /b 1
)

echo.
echo Build complete! Created: ollama-proxy.exe
echo.
echo To run the proxy:
echo   ollama-proxy.exe serve
echo   ollama-proxy.exe run phi4
echo.
echo To install as Windows service:
echo   install-service.bat
echo.