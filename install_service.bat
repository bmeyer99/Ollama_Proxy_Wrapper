@echo off
REM Install Ollama Metrics Proxy Windows Service
REM Run as Administrator

echo =========================================================
echo  Installing Ollama Metrics Proxy Service
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

REM Check if Python is available
python --version >nul 2>&1
if %errorLevel% neq 0 (
    echo ERROR: Python is not installed or not in PATH
    echo Please install Python 3.7+ and add it to your PATH
    pause
    exit /b 1
)

echo Checking Python version...
python -c "import sys; print(f'Python {sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}')"

REM Install required packages
echo Installing required Python packages...
pip install pywin32 aiohttp prometheus-client

if %errorLevel% neq 0 (
    echo ERROR: Failed to install required packages
    pause
    exit /b 1
)

REM Install the service
echo Installing Windows service...
python ollama_service.py install

if %errorLevel% neq 0 (
    echo ERROR: Failed to install service
    pause
    exit /b 1
)

REM Set service to start automatically
echo Configuring service for automatic startup...
sc config OllamaMetricsProxy start= auto

REM Start the service
echo Starting service...
python ollama_service.py start

if %errorLevel% neq 0 (
    echo WARNING: Service installed but failed to start
    echo You can start it manually from Services or run:
    echo   python ollama_service.py start
) else (
    echo =========================================================
    echo  SUCCESS: Ollama Metrics Proxy Service Installed!
    echo =========================================================
    echo.
    echo Service Name: OllamaMetricsProxy
    echo Proxy URL: http://localhost:11434
    echo Metrics URL: http://localhost:11434/metrics
    echo Analytics URL: http://localhost:11434/analytics/stats
    echo.
    echo The service will start automatically on boot.
    echo Look for the system tray icon when the service is running.
    echo.
    echo To uninstall: run uninstall_service.bat as Administrator
)

echo.
pause