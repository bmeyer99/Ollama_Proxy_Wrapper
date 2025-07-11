@echo off
REM Uninstall Ollama Metrics Proxy Windows Service
REM Run as Administrator

echo =========================================================
echo  Uninstalling Ollama Metrics Proxy Service
echo =========================================================

REM Check if running as administrator
net session >nul 2>&1
if %errorLevel% == 0 (
    echo Running with administrator privileges - OK
) else (
    echo ERROR: This script must be run as Administrator
    echo Right-click this file and select "Run as administrator"
    pause
    exit /b 1
)

REM Stop the service first
echo Stopping service...
python ollama_service.py stop
sc stop OllamaMetricsProxy

REM Wait a moment for service to stop
timeout /t 3 /nobreak >nul

REM Remove the service
echo Removing Windows service...
python ollama_service.py remove

if %errorLevel% neq 0 (
    echo WARNING: Python removal failed, trying sc delete...
    sc delete OllamaMetricsProxy
)

echo =========================================================
echo  Ollama Metrics Proxy Service Uninstalled
echo =========================================================
echo.
echo The service has been removed from Windows.
echo.
echo Note: This does not uninstall Python packages.
echo If you want to remove the Python dependencies, run:
echo   pip uninstall pywin32 aiohttp prometheus-client
echo.

pause