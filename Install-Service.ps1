#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Installs the Ollama Metrics Proxy as a Windows Service
.DESCRIPTION
    This script installs the Ollama Metrics Proxy service using WinSW for reliable
    service management. The service runs on startup and provides metrics collection
    for Ollama API calls.
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
Write-Host " Ollama Metrics Proxy Service Installer" -ForegroundColor Cyan
Write-Host "============================================`n" -ForegroundColor Cyan

# Check if running as administrator
if (-not ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole] "Administrator")) {
    throw "This script must be run as Administrator"
}

# Check if ollama-proxy.exe exists, build if needed
$exePath = Join-Path $scriptPath "ollama-proxy.exe"
if (-not (Test-Path $exePath)) {
    Write-Log "ollama-proxy.exe not found. Building it now..."
    
    # Check if Go is installed
    try {
        $goVersion = & go version 2>$null
        Write-Log "Found Go: $goVersion"
    } catch {
        Write-Log "ERROR: Go is not installed!" "ERROR"
        Write-Log "Please install Go from https://go.dev/dl/" "ERROR"
        exit 1
    }
    
    # Build the executable
    Write-Log "Downloading dependencies..."
    & go mod download
    if ($LASTEXITCODE -ne 0) {
        Write-Log "ERROR: Failed to download Go dependencies!" "ERROR"
        exit 1
    }
    
    Write-Log "Building ollama-proxy.exe..."
    & go build -ldflags="-w -s" -o ollama-proxy.exe .
    if ($LASTEXITCODE -ne 0) {
        Write-Log "ERROR: Failed to build ollama-proxy.exe!" "ERROR"
        exit 1
    }
    
    if (Test-Path $exePath) {
        Write-Log "Successfully built ollama-proxy.exe" "SUCCESS"
    } else {
        Write-Log "ERROR: Build completed but ollama-proxy.exe not found!" "ERROR"
        exit 1
    }
}

# Find Ollama executable BEFORE installing service
Write-Log "Locating Ollama executable..."
$ollamaPath = $null

# Check PATH first
try {
    $ollamaPath = (Get-Command ollama -ErrorAction Stop).Source
    Write-Log "Found Ollama in PATH: $ollamaPath"
} catch {
    # Check common locations
    $commonPaths = @(
        "C:\Program Files\Ollama\ollama.exe",
        "C:\Program Files (x86)\Ollama\ollama.exe",
        "$env:USERPROFILE\AppData\Local\Programs\Ollama\ollama.exe"
    )
    
    foreach ($path in $commonPaths) {
        if (Test-Path $path) {
            $ollamaPath = $path
            Write-Log "Found Ollama at: $ollamaPath"
            break
        }
    }
}

if (-not $ollamaPath) {
    Write-Log "ERROR: Ollama executable not found!" "ERROR"
    Write-Log "Please install Ollama first or ensure it's in PATH" "ERROR"
    exit 1
}

# Stop and remove existing service if present
$serviceName = "OllamaMetricsProxy"
$existingService = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($existingService) {
    Write-Log "Found existing service, removing..."
    
    if ($existingService.Status -eq 'Running') {
        Stop-Service -Name $serviceName -Force
        Start-Sleep -Seconds 2
    }
    
    # Try WinSW uninstall first
    $winswPath = Join-Path $scriptPath "winsw.exe"
    if (Test-Path $winswPath) {
        & $winswPath uninstall | Out-Null
        Start-Sleep -Seconds 2
    }
    
    # Then use sc delete as backup
    & sc.exe delete $serviceName | Out-Null
    Start-Sleep -Seconds 2
}

# Kill any existing Ollama processes
Write-Log "Checking for existing Ollama processes..."
$ollamaProcesses = Get-Process -Name "ollama" -ErrorAction SilentlyContinue
if ($ollamaProcesses) {
    Write-Log "Stopping existing Ollama processes..."
    $ollamaProcesses | Stop-Process -Force
    Start-Sleep -Seconds 2
}

# We don't need WinSW - the Go executable has native Windows service support
# Skip WinSW download and use native service installation
if ($false) { # Disabled WinSW path
    Write-Log "Downloading WinSW (Windows Service Wrapper)..."
    try {
        $winswUrl = "https://github.com/winsw/winsw/releases/download/v2.12.0/WinSW.NET4.exe"
        Invoke-WebRequest -Uri $winswUrl -OutFile $winswPath -UseBasicParsing
        Write-Log "WinSW downloaded successfully"
    } catch {
        Write-Log "Failed to download WinSW: $($_.Exception.Message)" "ERROR"
        Write-Log "Falling back to sc.exe installation..." "WARN"
        
        # Fallback to sc.exe
        $installCmd = "sc.exe create $serviceName binPath= `"$exePath -service`" start= delayed-auto DisplayName= `"Ollama Metrics Proxy`""
        Invoke-Expression $installCmd
        
        if ($LASTEXITCODE -eq 0) {
            sc.exe description $serviceName "Transparent metrics proxy for Ollama with Prometheus monitoring and analytics"
            sc.exe failure $serviceName reset= 86400 actions= restart/5000/restart/10000/restart/30000
            
            # Set Ollama path in service environment
            Write-Log "Setting service environment variables..."
            $regPath = "HKLM:\SYSTEM\CurrentControlSet\Services\$serviceName"
            New-ItemProperty -Path $regPath -Name "Environment" -Value @("OLLAMA_EXECUTABLE_PATH=$ollamaPath") -PropertyType MultiString -Force | Out-Null
            
            Write-Log "Service installed with sc.exe"
            Start-Service -Name $serviceName
            
            Write-Host "`n============================================" -ForegroundColor Green
            Write-Host " Service installed successfully!" -ForegroundColor Green
            Write-Host "============================================" -ForegroundColor Green
            Write-Host ""
            Write-Host "Service Name: $serviceName"
            Write-Host "Status: Running"
            Write-Host ""
            Write-Host "The proxy is now running on:"
            Write-Host "  http://localhost:11434" -ForegroundColor Yellow
            Write-Host ""
            Write-Host "View metrics at:"
            Write-Host "  http://localhost:11434/metrics" -ForegroundColor Yellow
            Write-Host ""
            Write-Host "View analytics at:"
            Write-Host "  http://localhost:11434/analytics" -ForegroundColor Yellow
            Write-Host ""
        } else {
            throw "Failed to create service"
        }
        exit 0
    }
}

# Prepare installation environment
$prepareScript = Join-Path $scriptPath "prepare-installation.bat"
if (Test-Path $prepareScript) {
    Write-Log "Preparing installation environment..."
    & $prepareScript | Out-Null
}

# Create logs directory if it doesn't exist
$logsDir = Join-Path $scriptPath "logs"
if (-not (Test-Path $logsDir)) {
    New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
    Write-Log "Created logs directory"
}

# Install using WinSW
Write-Log "Installing service with WinSW..."

# Update XML config with Ollama path
$xmlConfig = Join-Path $scriptPath "ollama-service.xml"
if (Test-Path $xmlConfig) {
    $xml = [xml](Get-Content $xmlConfig)
    
    # Add or update Ollama path environment variable
    $ollamaEnvVar = $xml.service.env | Where-Object { $_.name -eq "OLLAMA_EXECUTABLE_PATH" }
    if ($ollamaEnvVar) {
        $ollamaEnvVar.value = $ollamaPath
    } else {
        $newEnv = $xml.CreateElement("env")
        $newEnv.SetAttribute("name", "OLLAMA_EXECUTABLE_PATH")
        $newEnv.SetAttribute("value", $ollamaPath)
        $xml.service.AppendChild($newEnv) | Out-Null
    }
    
    $xml.Save($xmlConfig)
    Write-Log "Updated XML config with Ollama path: $ollamaPath"
}

# Use native Windows service installation
Write-Log "Installing service using native Windows service support..."

# Create service using sc.exe with the actual Go executable
# CRITICAL: sc.exe needs escaped quotes in the binPath
$serviceBinary = "`"$exePath`" -service"
Write-Log "Creating service with binary path: $serviceBinary"

# Delete existing service first to ensure clean state
& sc.exe delete $serviceName 2>$null | Out-Null

# Create the service with properly escaped quotes
# Run as LocalSystem for full permissions
$result = & sc.exe create $serviceName binPath= $serviceBinary start= delayed-auto DisplayName= "Ollama Metrics Proxy" obj= LocalSystem 2>&1
Write-Log "SC Create Result: $result"

if ($LASTEXITCODE -eq 0) {
    Write-Log "Service created successfully"
    
    # Set service description
    & sc.exe description $serviceName "Transparent metrics proxy for Ollama with Prometheus monitoring and analytics" | Out-Null
    
    # Configure service recovery
    & sc.exe failure $serviceName reset= 86400 actions= restart/5000/restart/10000/restart/30000 | Out-Null
    
    # Set Ollama path in registry for the service
    $regPath = "HKLM:\SYSTEM\CurrentControlSet\Services\$serviceName"
    New-ItemProperty -Path $regPath -Name "Environment" -Value @(
        "OLLAMA_EXECUTABLE_PATH=$ollamaPath",
        "OLLAMA_HOST=0.0.0.0:11435",
        "ANALYTICS_BACKEND=sqlite",
        "ANALYTICS_DIR=$scriptPath\ollama_analytics"
    ) -PropertyType MultiString -Force | Out-Null
    
    Write-Log "Service configuration completed"
    
    # Create event log source for the service
    Write-Log "Creating event log source..."
    try {
        New-EventLog -LogName Application -Source $serviceName -ErrorAction SilentlyContinue
        Write-Log "Event log source created"
    } catch {
        Write-Log "Event log source may already exist: $_" "WARN"
    }
    
    # Verify service configuration
    Write-Log "Verifying service configuration..."
    $serviceConfig = & sc.exe qc $serviceName 2>&1 | Out-String
    Write-Log "Service configuration:
$serviceConfig"
} else {
    Write-Log "Failed to create service: $result" "ERROR"
    throw "Service creation failed"
}

# Create empty log files to prevent WinSW rotation errors
$logFiles = @(
    "OllamaMetricsProxy.out.log",
    "OllamaMetricsProxy.err.log",
    "OllamaMetricsProxy.wrapper.log",
    "logs\OllamaMetricsProxy.log"
)

foreach ($logFile in $logFiles) {
    $logPath = Join-Path $scriptPath $logFile
    $logDir = Split-Path -Parent $logPath
    
    # Create directory if needed
    if (-not (Test-Path $logDir)) {
        New-Item -ItemType Directory -Path $logDir -Force | Out-Null
    }
    
    # Create empty file if it doesn't exist
    if (-not (Test-Path $logPath)) {
        New-Item -ItemType File -Path $logPath -Force | Out-Null
        Write-Log "Created log file: $logFile"
    }
}

# Service is now installed
if ($true) {
    Write-Log "Service installed successfully"

    # Wait for ports to be fully released from previous instance
    Write-Log "Waiting for ports to be released..."
    Start-Sleep -Seconds 5

    # Start the service
    Write-Log "Starting service..."
    try {
        Start-Service -Name $serviceName
    } catch {
        Write-Log "Service start failed: $($_.Exception.Message)" "ERROR"
        
        # Get detailed service status
        Write-Log "Getting detailed service status..." "INFO"
        $serviceStatus = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
        if ($serviceStatus) {
            Write-Log "Service Status: $($serviceStatus.Status)" "INFO"
            Write-Log "Service StartType: $($serviceStatus.StartType)" "INFO"
        }
        
        # Try to manually run the executable to see what happens
        Write-Log "Testing executable manually..." "INFO"
        try {
            $testOutput = & $exePath -service 2>&1
            Write-Log "Manual test output: $testOutput" "INFO"
        } catch {
            Write-Log "Manual test failed: $($_.Exception.Message)" "ERROR"
        }
        
        # Check if executable exists and is valid
        if (Test-Path $exePath) {
            $fileInfo = Get-Item $exePath
            Write-Log "Executable size: $($fileInfo.Length) bytes" "INFO"
            Write-Log "Executable path: $exePath" "INFO"
        } else {
            Write-Log "ERROR: Executable not found at $exePath" "ERROR"
        }
        
        # Check WinSW logs - try multiple possible log file names
        Write-Log "Looking for log files in: $scriptPath" "INFO"
        
        # List ALL files in directory to see what's actually there
        Write-Log "Files in directory:" "INFO"
        Get-ChildItem $scriptPath -Filter "*.log" | ForEach-Object {
            Write-Log "  Found: $($_.Name) (Size: $($_.Length) bytes)" "INFO"
        }
        
        $possibleLogs = @(
            "OllamaMetricsProxy.err.log",
            "OllamaMetricsProxy.out.log", 
            "OllamaMetricsProxy.wrapper.log",
            "winsw.err.log",
            "winsw.out.log",
            "*.log"  # Check ANY log file
        )
        
        foreach ($logFile in $possibleLogs) {
            $logPath = Join-Path $scriptPath $logFile
            if (Test-Path $logPath) {
                Write-Log "Found log file: $logFile" "INFO"
                $logContent = Get-Content $logPath -ErrorAction SilentlyContinue
                if ($logContent) {
                    Write-Log "Content of $logFile (last 20 lines):" "INFO"
                    $logContent | Select-Object -Last 20 | ForEach-Object {
                        Write-Log "  $_" "INFO"
                    }
                } else {
                    Write-Log "Log file $logFile is empty" "INFO"
                }
            }
        }
        
        # Try to get more service details
        Write-Log "Running sc query for detailed service info..." "INFO"
        $scOutput = & sc.exe query $serviceName 2>&1
        Write-Log "SC Query Output: $scOutput" "INFO"
        
        # Check if Ollama is available
        Write-Log "Checking Ollama availability..." "INFO"
        if (Test-Path $ollamaPath) {
            Write-Log "Ollama found at: $ollamaPath" "INFO"
            try {
                $ollamaVersion = & $ollamaPath --version 2>&1
                Write-Log "Ollama version: $ollamaVersion" "INFO"
            } catch {
                Write-Log "Failed to get Ollama version: $($_.Exception.Message)" "ERROR"
            }
        } else {
            Write-Log "ERROR: Ollama not found at: $ollamaPath" "ERROR"
        }
        
        throw "Service failed to start"
    }
    
    # Wait for service to start
    $timeout = 30
    $elapsed = 0
    while ((Get-Service -Name $serviceName).Status -ne 'Running' -and $elapsed -lt $timeout) {
        Start-Sleep -Seconds 1
        $elapsed++
    }
    
    if ((Get-Service -Name $serviceName).Status -eq 'Running') {
        Write-Host "`n============================================" -ForegroundColor Green
        Write-Host " Service installed successfully!" -ForegroundColor Green
        Write-Host "============================================" -ForegroundColor Green
        Write-Host ""
        Write-Host "Service Name: $serviceName"
        Write-Host "Status: Running"
        Write-Host ""
        Write-Host "The proxy is now running on:"
        Write-Host "  http://localhost:11434" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "View metrics at:"
        Write-Host "  http://localhost:11434/metrics" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "View analytics at:"
        Write-Host "  http://localhost:11434/analytics" -ForegroundColor Yellow
        Write-Host ""
        
        # Test the proxy
        Write-Log "Testing proxy connectivity..."
        try {
            $response = Invoke-WebRequest -Uri "http://localhost:11434/test" -UseBasicParsing -TimeoutSec 5
            if ($response.StatusCode -eq 200) {
                Write-Log "Proxy is responding correctly" "SUCCESS"
            }
        } catch {
            Write-Log "Warning: Proxy test failed, but service is running" "WARN"
        }
    } else {
        throw "Service failed to start within $timeout seconds"
    }
} else {
    throw "Failed to install service"
}

Write-Host "`nPress any key to exit..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")