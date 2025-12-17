#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Uninstalls the Ollama Metrics Proxy Windows Service
.DESCRIPTION
    This script removes the Ollama Metrics Proxy service and cleans up
    all related files.
#>

# Script setup
$ErrorActionPreference = "Stop"
$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptPath

# Logging function
function Write-Log {
    param([string]$Message, [string]$Level = "INFO")
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $logMessage = "[$timestamp] [$Level] $Message"
    Write-Host $logMessage
}

Write-Host "`n============================================" -ForegroundColor Cyan
Write-Host " Ollama Metrics Proxy Service Uninstaller" -ForegroundColor Cyan
Write-Host "============================================`n" -ForegroundColor Cyan

# Check if running as administrator
if (-not ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole] "Administrator")) {
    throw "This script must be run as Administrator"
}

$serviceName = "OllamaMetricsProxy"

# Check if service exists
$service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if (-not $service) {
    Write-Log "Service '$serviceName' is not installed" "WARN"
    exit 0
}

# Stop the service if running
if ($service.Status -eq 'Running') {
    Write-Log "Stopping service..."
    Stop-Service -Name $serviceName -Force

    # Wait for service to stop (graceful shutdown can take up to 10 seconds)
    $timeout = 30
    $elapsed = 0
    while ((Get-Service -Name $serviceName).Status -ne 'Stopped' -and $elapsed -lt $timeout) {
        Start-Sleep -Seconds 1
        $elapsed++
    }

    if ((Get-Service -Name $serviceName).Status -ne 'Stopped') {
        Write-Log "Warning: Service did not stop gracefully" "WARN"
    }

    # Additional delay to allow cleanup to complete
    Write-Log "Waiting for cleanup to complete..."
    Start-Sleep -Seconds 5
}

# Uninstall using WinSW if available
$winswExe = Join-Path $scriptPath "OllamaMetricsProxy.exe"
if (Test-Path $winswExe) {
    Write-Log "Uninstalling service with WinSW..."
    & $winswExe uninstall | Out-Null
    Start-Sleep -Seconds 2
}

# Use sc.exe as backup
Write-Log "Removing service registration..."
& sc.exe delete $serviceName | Out-Null

if ($LASTEXITCODE -eq 0) {
    Write-Log "Service uninstalled successfully"
} else {
    Write-Log "Warning: sc.exe delete returned error code $LASTEXITCODE" "WARN"
}

# Clean up WinSW files
$filesToClean = @(
    "OllamaMetricsProxy.exe",
    "OllamaMetricsProxy.exe.config",
    "OllamaMetricsProxy.out.log",
    "OllamaMetricsProxy.err.log",
    "OllamaMetricsProxy.wrapper.log",
    "winsw.exe"
)

Write-Log "Cleaning up service files..."
foreach ($file in $filesToClean) {
    $filePath = Join-Path $scriptPath $file
    if (Test-Path $filePath) {
        try {
            Remove-Item $filePath -Force
            Write-Log "Removed: $file"
        } catch {
            Write-Log "Failed to remove $file : $($_.Exception.Message)" "WARN"
        }
    }
}

Write-Host "`n============================================" -ForegroundColor Green
Write-Host " Service uninstalled successfully!" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Green
Write-Host ""
Write-Host "The Ollama Metrics Proxy service has been removed."
Write-Host ""
Write-Host "Note: The proxy executable and analytics data have been preserved."
Write-Host "      Delete them manually if no longer needed."
Write-Host ""

Write-Host "`nPress any key to exit..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")