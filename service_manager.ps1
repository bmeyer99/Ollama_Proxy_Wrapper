# Ollama Metrics Proxy Service Manager
# PowerShell script for managing the Windows service

param(
    [Parameter(Position=0)]
    [ValidateSet("install", "uninstall", "start", "stop", "restart", "status", "test")]
    [string]$Action = "status"
)

$ServiceName = "OllamaMetricsProxy"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ServiceScript = Join-Path $ScriptDir "ollama_service.py"

Write-Host "=========================================================" -ForegroundColor Cyan
Write-Host "  Ollama Metrics Proxy Service Manager" -ForegroundColor Cyan  
Write-Host "=========================================================" -ForegroundColor Cyan

# Check if running as administrator for install/uninstall operations
function Test-Administrator {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]($currentUser)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

# Install service
function Install-Service {
    if (-not (Test-Administrator)) {
        Write-Host "ERROR: Administrator privileges required for installation" -ForegroundColor Red
        Write-Host "Please run PowerShell as Administrator" -ForegroundColor Yellow
        return $false
    }
    
    Write-Host "Installing dependencies..." -ForegroundColor Yellow
    try {
        & pip install pywin32 aiohttp prometheus-client
        if ($LASTEXITCODE -ne 0) {
            throw "pip install failed"
        }
    } catch {
        Write-Host "ERROR: Failed to install Python dependencies" -ForegroundColor Red
        return $false
    }
    
    Write-Host "Installing Windows service..." -ForegroundColor Yellow
    try {
        & python $ServiceScript install
        if ($LASTEXITCODE -ne 0) {
            throw "Service installation failed"
        }
        
        # Configure for automatic startup
        & sc.exe config $ServiceName start= auto
        
        Write-Host "SUCCESS: Service installed successfully!" -ForegroundColor Green
        Write-Host "Service will start automatically on boot" -ForegroundColor Green
        return $true
    } catch {
        Write-Host "ERROR: Failed to install service: $_" -ForegroundColor Red
        return $false
    }
}

# Uninstall service
function Uninstall-Service {
    if (-not (Test-Administrator)) {
        Write-Host "ERROR: Administrator privileges required for uninstallation" -ForegroundColor Red
        return $false
    }
    
    Write-Host "Stopping service..." -ForegroundColor Yellow
    try {
        & python $ServiceScript stop
        & sc.exe stop $ServiceName
    } catch {
        Write-Host "Service may already be stopped" -ForegroundColor Yellow
    }
    
    Start-Sleep -Seconds 3
    
    Write-Host "Removing service..." -ForegroundColor Yellow
    try {
        & python $ServiceScript remove
        if ($LASTEXITCODE -ne 0) {
            & sc.exe delete $ServiceName
        }
        Write-Host "SUCCESS: Service uninstalled" -ForegroundColor Green
        return $true
    } catch {
        Write-Host "ERROR: Failed to remove service: $_" -ForegroundColor Red
        return $false
    }
}

# Start service
function Start-Service {
    try {
        & python $ServiceScript start
        if ($LASTEXITCODE -eq 0) {
            Write-Host "SUCCESS: Service started" -ForegroundColor Green
            Show-ServiceInfo
        } else {
            Write-Host "ERROR: Failed to start service" -ForegroundColor Red
        }
    } catch {
        Write-Host "ERROR: $_" -ForegroundColor Red
    }
}

# Stop service
function Stop-Service {
    try {
        & python $ServiceScript stop
        Write-Host "Service stopped" -ForegroundColor Yellow
    } catch {
        Write-Host "ERROR: $_" -ForegroundColor Red
    }
}

# Show service info
function Show-ServiceInfo {
    Write-Host "`nService Information:" -ForegroundColor Cyan
    Write-Host "  Proxy URL: http://localhost:11434" -ForegroundColor White
    Write-Host "  Metrics: http://localhost:11434/metrics" -ForegroundColor White  
    Write-Host "  Analytics: http://localhost:11434/analytics/stats" -ForegroundColor White
    Write-Host "  Log Location: %TEMP%\ollama_service\ollama_service.log" -ForegroundColor White
}

# Get service status
function Get-ServiceStatus {
    try {
        $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
        if ($service) {
            Write-Host "Service Status: $($service.Status)" -ForegroundColor $(
                if ($service.Status -eq "Running") { "Green" } else { "Yellow" }
            )
            if ($service.Status -eq "Running") {
                Show-ServiceInfo
            }
        } else {
            Write-Host "Service is not installed" -ForegroundColor Yellow
            Write-Host "Run: .\service_manager.ps1 install" -ForegroundColor Cyan
        }
    } catch {
        Write-Host "Error checking service status: $_" -ForegroundColor Red
    }
}

# Test in console mode
function Test-Service {
    Write-Host "Starting service in console mode for testing..." -ForegroundColor Yellow
    Write-Host "Press Ctrl+C to stop`n" -ForegroundColor Yellow
    
    try {
        & python $ServiceScript
    } catch {
        Write-Host "Test completed" -ForegroundColor Yellow
    }
}

# Main execution
switch ($Action.ToLower()) {
    "install" {
        if (Install-Service) {
            Start-Service
        }
    }
    "uninstall" {
        Uninstall-Service
    }
    "start" {
        Start-Service
    }
    "stop" {
        Stop-Service
    }
    "restart" {
        Stop-Service
        Start-Sleep -Seconds 2
        Start-Service
    }
    "test" {
        Test-Service
    }
    "status" {
        Get-ServiceStatus
    }
    default {
        Write-Host "Usage: .\service_manager.ps1 [install|uninstall|start|stop|restart|status|test]" -ForegroundColor Yellow
        Get-ServiceStatus
    }
}

Write-Host ""