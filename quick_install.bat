@echo off
REM Quick installer for Ollama Transparent Metrics

echo ================================================
echo    Ollama Transparent Metrics - Quick Install
echo ================================================
echo.
echo This will set up transparent metrics collection
echo for Ollama without changing how you use it.
echo.
pause

REM Check Python
python --version >nul 2>&1
if errorlevel 1 (
    echo ERROR: Python is not installed
    echo.
    echo Please install Python first from:
    echo https://www.python.org/downloads/
    echo.
    echo Make sure to check "Add Python to PATH"
    pause
    exit /b 1
)

echo ✓ Python found
echo.

REM Install dependencies
echo Installing required Python packages...
pip install aiohttp prometheus-client
if errorlevel 1 (
    echo ERROR: Failed to install packages
    pause
    exit /b 1
)

echo ✓ Packages installed
echo.

REM Check if all files exist
set MISSING=0
if not exist "ollama_hybrid_proxy.py" (
    echo ✗ Missing: ollama_hybrid_proxy.py
    set MISSING=1
)
if not exist "ollama_wrapper.py" (
    echo ✗ Missing: ollama_wrapper.py
    set MISSING=1
)

if %MISSING%==1 (
    echo.
    echo ERROR: Missing required files
    echo Please ensure all .py files are in this directory
    pause
    exit /b 1
)

echo ✓ All files found
echo.

REM Create convenient launcher
echo Creating convenient launcher...
(
echo @echo off
echo REM Set environment variable for Ollama port configuration
echo set OLLAMA_HOST=0.0.0.0:11435
echo python "%~dp0ollama_wrapper.py" %%*
) > ollama_metrics.bat

echo ✓ Created ollama_metrics.bat
echo.

REM Test the setup
echo Testing the setup...
python ollama_wrapper.py --version >nul 2>&1
if errorlevel 1 (
    echo ⚠ Warning: Test failed, but setup is complete
) else (
    echo ✓ Test passed
)

echo.
echo ================================================
echo    Installation Complete!
echo ================================================
echo.
echo Usage:
echo   ollama_metrics.bat [any ollama command]
echo.
echo Examples:
echo   ollama_metrics.bat list
echo   ollama_metrics.bat run phi4
echo   ollama_metrics.bat serve
echo.
echo Metrics will be available at:
echo   http://localhost:11434/metrics
echo   http://localhost:11434/analytics/stats
echo.
echo Optional: Add %CD% to your PATH to use from anywhere
echo.
pause