@echo off
:: Uninstall Ollama Proxy Windows Service
:: Requires administrator privileges

echo ============================================
echo  Ollama Proxy Service Uninstallation
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

:: Check if service exists
sc query OllamaMetricsProxy >nul 2>&1
if %errorlevel% neq 0 (
    echo Service OllamaMetricsProxy is not installed.
    pause
    exit /b 0
)

:: Stop the service
echo Stopping service...
net stop OllamaMetricsProxy >nul 2>&1

:: Delete the service
echo Removing service...
sc delete OllamaMetricsProxy

if %errorlevel% equ 0 (
    echo.
    echo ============================================
    echo  Service uninstalled successfully!
    echo ============================================
    echo.
) else (
    echo Failed to uninstall service!
)

pause