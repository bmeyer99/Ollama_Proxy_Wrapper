@echo off
:: Install Ollama Proxy as Windows Service
:: Requires administrator privileges

echo ============================================
echo  Ollama Proxy Service Installation
echo ============================================
echo.

:: Check for admin rights
net session >nul 2>&1
if %errorlevel% neq 0 (
    echo This script requires administrator privileges.
    echo Please run as Administrator.
    pause
    exit /b 1
)

:: Check if executable exists
if not exist "%~dp0ollama-proxy.exe" (
    echo ERROR: ollama-proxy.exe not found!
    echo Please run build.bat first.
    pause
    exit /b 1
)

:: Stop existing service if running
echo Checking for existing service...
sc query OllamaMetricsProxy >nul 2>&1
if %errorlevel% equ 0 (
    echo Stopping existing service...
    net stop OllamaMetricsProxy >nul 2>&1
    sc delete OllamaMetricsProxy >nul 2>&1
    timeout /t 2 >nul
)

:: Create the service
echo Installing service...
sc create OllamaMetricsProxy binPath= "\"%~dp0ollama-proxy.exe\" --service" start= auto DisplayName= "Ollama Metrics Proxy"

if %errorlevel% neq 0 (
    echo Failed to create service!
    pause
    exit /b 1
)

:: Set service description
sc description OllamaMetricsProxy "Transparent metrics proxy for Ollama that adds Prometheus monitoring and analytics collection"

:: Configure service recovery
sc failure OllamaMetricsProxy reset= 86400 actions= restart/5000/restart/10000/restart/30000

:: Start the service
echo Starting service...
net start OllamaMetricsProxy

if %errorlevel% equ 0 (
    echo.
    echo ============================================
    echo  Service installed successfully!
    echo ============================================
    echo.
    echo Service Name: OllamaMetricsProxy
    echo Status: Running
    echo.
    echo The proxy is now running on:
    echo   http://localhost:11434
    echo.
    echo View metrics at:
    echo   http://localhost:11434/metrics
    echo.
    echo View analytics at:
    echo   http://localhost:11434/analytics
    echo.
) else (
    echo.
    echo Service installed but failed to start.
    echo Check Event Viewer for details.
)

pause