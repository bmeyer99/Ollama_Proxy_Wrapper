#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Installs the Ollama Metrics Proxy as a Windows Service
.DESCRIPTION
    This script installs the Ollama Metrics Proxy service that runs on startup
    and provides metrics collection for Ollama API calls.
#>

# Script setup
$ErrorActionPreference = "Stop"
$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptPath

# Import utilities
. "$scriptPath\ServiceUtilities.ps1"
Import-Module "$scriptPath\OllamaManager.psm1" -Force

# Installation functions
function Install-WinSWService {
    Write-Log "Installing with WinSW (Windows Service Wrapper)..."
    
    # Download WinSW if not present
    $winswPath = Join-Path $scriptPath "winsw.exe"
    if (-not (Test-Path $winswPath)) {
        Write-Log "Downloading WinSW..."
        try {
            $winswUrl = "https://github.com/winsw/winsw/releases/download/v3.0.0-alpha.11/WinSW-x64.exe"
            Invoke-WebRequest -Uri $winswUrl -OutFile $winswPath -UseBasicParsing
            Write-Log "WinSW downloaded successfully"
        } catch {
            throw "Failed to download WinSW: $($_.Exception.Message)"
        }
    }
    
    # Update configuration file with correct Python path
    $configPath = Join-Path $scriptPath "ollama-service.xml"
    if (-not (Test-Path $configPath)) {
        throw "Service configuration file not found: $configPath"
    }
    
    # Get Python executable path
    $pythonPath = (Get-Command python).Source
    Write-Log "Using Python path: $pythonPath"
    
    # Get Ollama executable path
    $ollamaPath = $ollamaInfo.Path
    Write-Log "Using Ollama path: $ollamaPath"
    
    # Update XML with correct paths
    $xmlContent = Get-Content $configPath -Raw
    $xmlContent = $xmlContent -replace "PYTHON_PATH_PLACEHOLDER", $pythonPath
    $xmlContent = $xmlContent -replace "OLLAMA_PATH_PLACEHOLDER", $ollamaPath
    Set-Content $configPath $xmlContent
    
    # Install service using WinSW
    Write-Log "Installing service with WinSW..."
    Write-Log "Command: $winswPath install $configPath"
    
    try {
        $process = Start-Process -FilePath $winswPath -ArgumentList "install", "`"$configPath`"" -Wait -PassThru -RedirectStandardOutput "$env:TEMP\winsw_install_stdout.txt" -RedirectStandardError "$env:TEMP\winsw_install_stderr.txt" -NoNewWindow
        
        $stdout = Get-Content "$env:TEMP\winsw_install_stdout.txt" -ErrorAction SilentlyContinue
        $stderr = Get-Content "$env:TEMP\winsw_install_stderr.txt" -ErrorAction SilentlyContinue
        
        Write-Log "WinSW install STDOUT:"
        if ($stdout) { 
            foreach ($line in $stdout) { Write-Log "  $line" }
        } else {
            Write-Log "  (no stdout output)"
        }
        
        Write-Log "WinSW install STDERR:"
        if ($stderr) { 
            foreach ($line in $stderr) { Write-Log "  $line" }
        } else {
            Write-Log "  (no stderr output)"
        }
        
        Write-Log "WinSW process exit code: $($process.ExitCode)"
        
        if ($process.ExitCode -ne 0) {
            throw "WinSW service install failed with exit code: $($process.ExitCode)"
        }
        
        Write-Log "Service installed successfully with WinSW" -Level Success
        
    } catch {
        Write-Log "ERROR: Failed to install service with WinSW: $($_.Exception.Message)" -Level Error
        throw
    }
}


# Main execution
Write-Host "=========================================================" -ForegroundColor Cyan
Write-Host " Installing Ollama Metrics Proxy Service" -ForegroundColor Cyan
Write-Host "=========================================================" -ForegroundColor Cyan
Write-Host ""

# Initialize logging
$logFile = Initialize-Log -LogPrefix "service_install"

try {
    # Check prerequisites
    Test-AdminPrivileges
    Test-PythonInstallation
    
    # Check if Ollama is installed
    Write-Log "Checking Ollama installation..."
    $ollamaInfo = Test-OllamaInstallation
    if (-not $ollamaInfo.Installed) {
        throw "Ollama is not installed. Please install Ollama first from https://ollama.ai"
    }
    Write-Log "Ollama found: $($ollamaInfo.Version)" -Level Success
    if ($ollamaInfo.NotInPath) {
        Write-Log "Note: Ollama not in PATH, using: $($ollamaInfo.Path)" -Level Warning
    }
    
    # Handle existing Ollama processes
    $ollamaProcesses = Get-OllamaProcess
    if ($ollamaProcesses.Count -gt 0) {
        Write-Log ""
        Write-Log "Found existing Ollama process(es) using port 11434" -Level Warning
        Write-Log "The metrics proxy needs to control this port."
        Write-Log ""
        
        $response = Read-Host "Stop existing Ollama processes? (Y/N)"
        if ($response -eq 'Y' -or $response -eq 'y') {
            if (-not (Stop-OllamaGracefully -Force)) {
                throw "Failed to stop existing Ollama processes"
            }
        } else {
            throw "Cannot proceed with existing Ollama processes running"
        }
    }
    
    # Now check port availability
    Test-PortAvailability -Port 11434 -PortName "proxy"
    
    # Handle auto-start configurations
    Write-Log ""
    $autoStartConfigs = Get-OllamaAutoStart
    if ($autoStartConfigs.Count -gt 0) {
        Write-Log "Found Ollama auto-start configuration(s):" -Level Warning
        foreach ($config in $autoStartConfigs) {
            Write-Log "  - $($config.Type): $($config.Name)"
        }
        Write-Log ""
        Write-Log "These need to be disabled so the metrics proxy can manage Ollama."
        Write-Log "They will be restored when you uninstall the metrics proxy."
        Write-Log ""
        
        $response = Read-Host "Disable Ollama auto-start? (Y/N)"
        if ($response -eq 'Y' -or $response -eq 'y') {
            if (-not (Disable-OllamaAutoStart)) {
                Write-Log "Warning: Some auto-start configs could not be disabled" -Level Warning
                Write-Log "You may need to disable them manually" -Level Warning
            }
        } else {
            Write-Log "Warning: Ollama auto-start may conflict with the metrics proxy" -Level Warning
        }
    }
    
    # Check required files
    $requiredFiles = @("ollama_wrapper.py", "ollama_fastapi_proxy.py", "ollama_runner.py", "ollama-service.xml")
    Test-RequiredFiles -Files $requiredFiles
    
    # Stop and remove existing service if present
    Write-Log "Checking for existing service..."
    $existingService = Get-Service -Name "OllamaMetricsProxy" -ErrorAction SilentlyContinue
    if ($existingService) {
        Write-Log "Found existing service, removing..."
        Stop-ServiceSafely -ServiceName "OllamaMetricsProxy"
        Remove-ServiceSafely -ServiceName "OllamaMetricsProxy"
    }
    
    # Install required Python packages (latest versions for Python 3.13 compatibility)
    Install-PythonPackages -Packages @("fastapi", "uvicorn", "httpx", "prometheus-client", "requests")
    
    # Install service using WinSW
    Install-WinSWService
    
    # Configure service
    Write-Log ""
    Write-Log "Configuring service..."
    Set-Service -Name "OllamaMetricsProxy" -StartupType Automatic
    
    # Set failure actions
    & sc.exe failure OllamaMetricsProxy reset= 86400 actions= restart/60000/restart/60000/restart/60000 | Out-Null
    
    # Start service
    Write-Log "Starting service..."
    Start-Service -Name "OllamaMetricsProxy"
    Start-Sleep -Seconds 3
    
    # Verify service is running
    $service = Get-Service -Name "OllamaMetricsProxy"
    if ($service.Status -ne "Running") {
        Write-Log ""
        Write-Log "WARNING: Service installed but not running" -Level Warning
        Write-Log "Service status: $($service.Status)"
    } else {
        Write-Log ""
        Write-Host "=========================================================" -ForegroundColor Green
        Write-Host " SUCCESS: Ollama Metrics Proxy Service Installed!" -ForegroundColor Green
        Write-Host "=========================================================" -ForegroundColor Green
        Write-Host ""
        Write-Host "Service Name: OllamaMetricsProxy" -ForegroundColor White
        Write-Host "Proxy URL: http://localhost:11434" -ForegroundColor White
        Write-Host "Metrics URL: http://localhost:11434/metrics" -ForegroundColor White
        Write-Host "Analytics URL: http://localhost:11434/analytics/stats" -ForegroundColor White
        Write-Host ""
        Write-Host "The service will start automatically on boot." -ForegroundColor Gray
        Write-Host ""
        Write-Host "To uninstall: run Uninstall-Service.ps1 as Administrator" -ForegroundColor Gray
    }
    
} catch {
    Write-Log ""
    Write-Log "ERROR: $($_.Exception.Message)" -Level Error
    Write-Log "Installation failed. Check the log: $logFile" -Level Error
    throw
} finally {
    Write-Host ""
    Write-Host "Log saved to: $logFile" -ForegroundColor Gray
    Write-Host ""
    Write-Host "Press any key to continue..."
    $null = $Host.UI.RawUI.ReadKey('NoEcho,IncludeKeyDown')
}