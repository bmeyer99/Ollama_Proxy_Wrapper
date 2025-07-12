#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Uninstalls the Ollama Metrics Proxy Windows Service
.DESCRIPTION
    This script removes the Ollama Metrics Proxy service from Windows.
    The proxy files remain installed for manual use.
#>

# Script setup
$ErrorActionPreference = "Stop"
$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptPath

# Import utilities
. "$scriptPath\ServiceUtilities.ps1"
Import-Module "$scriptPath\OllamaManager.psm1" -Force

Write-Host "=========================================================" -ForegroundColor Cyan
Write-Host " Uninstalling Ollama Metrics Proxy Service" -ForegroundColor Cyan
Write-Host "=========================================================" -ForegroundColor Cyan
Write-Host ""

# Initialize logging
$logFile = Initialize-Log -LogPrefix "service_uninstall"

try {
    # Check prerequisites
    Test-AdminPrivileges
    
    # Check if service exists
    Write-Log "Checking for service..."
    $service = Get-Service -Name "OllamaMetricsProxy" -ErrorAction SilentlyContinue
    
    if (-not $service) {
        Write-Log "Service 'OllamaMetricsProxy' not found."
        Write-Log "Nothing to uninstall."
        return
    }
    
    # Stop the service
    Write-Log "Stopping service..."
    Stop-ServiceSafely -ServiceName "OllamaMetricsProxy"
    
    # Use WinSW for removal
    $winswPath = Join-Path $scriptPath "winsw.exe"
    $configPath = Join-Path $scriptPath "ollama-service.xml"
    
    if ((Test-Path $winswPath) -and (Test-Path $configPath)) {
        Write-Log "Attempting WinSW service removal..."
        try {
            $process = Start-Process -FilePath $winswPath -ArgumentList "uninstall", "`"$configPath`"" -Wait -PassThru -RedirectStandardOutput "$env:TEMP\winsw_uninstall_stdout.txt" -RedirectStandardError "$env:TEMP\winsw_uninstall_stderr.txt" -NoNewWindow
            
            $stdout = Get-Content "$env:TEMP\winsw_uninstall_stdout.txt" -ErrorAction SilentlyContinue
            $stderr = Get-Content "$env:TEMP\winsw_uninstall_stderr.txt" -ErrorAction SilentlyContinue
            
            if ($stdout) { 
                foreach ($line in $stdout) { Write-Log "  $line" }
            }
            if ($stderr) { 
                foreach ($line in $stderr) { Write-Log "  $line" }
            }
            
            if ($process.ExitCode -eq 0) {
                Write-Log "WinSW service removal completed" -Level Success
            } else {
                Write-Log "WinSW service removal failed, using fallback" -Level Warning
                Remove-ServiceSafely -ServiceName "OllamaMetricsProxy"
            }
        } catch {
            Write-Log "WinSW service removal failed: $($_.Exception.Message)" -Level Warning
            Remove-ServiceSafely -ServiceName "OllamaMetricsProxy"
        }
    } else {
        Write-Log "WinSW not found, using manual removal"
        Remove-ServiceSafely -ServiceName "OllamaMetricsProxy"
    }
    
    # Verify removal
    Start-Sleep -Seconds 2
    $service = Get-Service -Name "OllamaMetricsProxy" -ErrorAction SilentlyContinue
    if ($service) {
        Write-Log ""
        Write-Log "ERROR: Failed to remove service" -Level Error
        Write-Log "You may need to restart Windows to complete removal" -Level Error
        throw "Service removal failed"
    } else {
        Write-Log ""
        Write-Host "=========================================================" -ForegroundColor Green
        Write-Host " Service Successfully Uninstalled" -ForegroundColor Green
        Write-Host "=========================================================" -ForegroundColor Green
        Write-Host ""
        Write-Host "The Ollama Metrics Proxy service has been removed." -ForegroundColor White
        
        # Restore Ollama auto-start configurations
        Write-Log ""
        Write-Log "Restoring Ollama auto-start configurations..."
        if (Restore-OllamaAutoStart) {
            Write-Log "Ollama will now start normally with Windows" -Level Success
        } else {
            Write-Log "Could not restore some auto-start configurations" -Level Warning
        }
        
        Write-Host ""
        Write-Host "Note: The proxy files are still installed." -ForegroundColor Gray
        Write-Host "You can still use: ollama_metrics.bat" -ForegroundColor Gray
        Write-Host ""
        Write-Host "To reinstall the service later:" -ForegroundColor Gray
        Write-Host "  Run Install-Service.ps1 as Administrator" -ForegroundColor Gray
        
        # Cleanup WinSW files if requested
        Write-Host ""
        $cleanupResponse = Read-Host "Remove WinSW files? (Y/N)"
        if ($cleanupResponse -eq 'Y' -or $cleanupResponse -eq 'y') {
            $winswPath = Join-Path $scriptPath "winsw.exe"
            if (Test-Path $winswPath) {
                Remove-Item $winswPath -Force
                Write-Log "WinSW executable removed"
                Write-Host "WinSW files removed." -ForegroundColor Green
            }
        }
    }
    
} catch {
    Write-Log ""
    Write-Log "ERROR: $($_.Exception.Message)" -Level Error
    throw
} finally {
    Write-Host ""
    Write-Host "Log saved to: $logFile" -ForegroundColor Gray
    Write-Host ""
    Write-Host "Press any key to continue..."
    $null = $Host.UI.RawUI.ReadKey('NoEcho,IncludeKeyDown')
}