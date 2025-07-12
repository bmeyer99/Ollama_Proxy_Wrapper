<#
.SYNOPSIS
    Shared utilities for Ollama Metrics Proxy service management
.DESCRIPTION
    Contains common functions used by Install-Service.ps1 and Uninstall-Service.ps1
#>

# Global variables
$script:LogFile = ""

function Initialize-Log {
    param(
        [string]$LogPrefix = "service"
    )
    
    $timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
    $script:LogFile = Join-Path $PSScriptRoot "${LogPrefix}_${timestamp}.txt"
    
    "Log started at $(Get-Date)" | Out-File -FilePath $script:LogFile
    "=" * 50 | Out-File -FilePath $script:LogFile -Append
    
    return $script:LogFile
}

function Write-Log {
    param(
        [string]$Message,
        [string]$Level = "Info"
    )
    
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $logMessage = "[$timestamp] [$Level] $Message"
    
    # Write to console with color
    switch ($Level) {
        "Error" { Write-Host $Message -ForegroundColor Red }
        "Warning" { Write-Host $Message -ForegroundColor Yellow }
        "Success" { Write-Host $Message -ForegroundColor Green }
        default { Write-Host $Message }
    }
    
    # Write to log file
    if ($script:LogFile) {
        $logMessage | Out-File -FilePath $script:LogFile -Append
    }
}

function Test-AdminPrivileges {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($currentUser)
    $isAdmin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    
    if (-not $isAdmin) {
        throw "Administrator privileges required. Right-click and select 'Run as administrator'"
    }
    
    Write-Log "Running with administrator privileges" -Level Success
}

function Test-PythonInstallation {
    Write-Log "Checking Python installation..."
    
    try {
        $pythonVersion = & python --version 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "Python not found"
        }
        Write-Log $pythonVersion
        
        # Get Python path and log it
        $pythonPath = (Get-Command python).Source
        Write-Log "Python executable found at: $pythonPath"
        
        # Only warn about Microsoft Store Python, don't fail
        if ($pythonPath -like "*WindowsApps*") {
            Write-Log ""
            Write-Log "WARNING: Microsoft Store Python detected!" -Level Warning
            Write-Log "Microsoft Store Python may have limitations with Windows Services." -Level Warning
            Write-Log "If service installation fails, consider installing Python from https://www.python.org/downloads/" -Level Warning
        }
        
    } catch {
        throw "Python is not installed or not in PATH. Please install from https://www.python.org/downloads/"
    }
}

function Test-PortAvailability {
    param(
        [int]$Port,
        [string]$PortName
    )
    
    $tcpConnection = Get-NetTCPConnection -LocalPort $Port -ErrorAction SilentlyContinue
    if ($tcpConnection) {
        Write-Log "WARNING: Port $Port ($PortName) is already in use!" -Level Warning
        Write-Log ""
        Write-Log "Process using port:"
        
        $process = Get-Process -Id $tcpConnection[0].OwningProcess -ErrorAction SilentlyContinue
        if ($process) {
            Write-Log "  Process: $($process.Name) (PID: $($process.Id))"
        }
        
        throw "Port $Port is already in use. Please stop any existing Ollama instances."
    }
}

function Test-RequiredFiles {
    param(
        [string[]]$Files
    )
    
    Write-Log "Checking required files..."
    $missing = @()
    
    foreach ($file in $Files) {
        $filePath = Join-Path $PSScriptRoot $file
        if (-not (Test-Path $filePath)) {
            $missing += $file
            Write-Log "Missing: $file" -Level Error
        }
    }
    
    if ($missing.Count -gt 0) {
        Write-Log ""
        Write-Log "ERROR: Missing required files in $PSScriptRoot" -Level Error
        Write-Log "Files found:"
        Get-ChildItem -Path $PSScriptRoot -Filter "*.py" | ForEach-Object {
            Write-Log "  $($_.Name)"
        }
        throw "Missing required files: $($missing -join ', ')"
    }
    
    Write-Log "All required files found" -Level Success
}

function Install-PythonPackages {
    param(
        [string[]]$Packages
    )
    
    Write-Log "Installing required Python packages..."
    
    foreach ($package in $Packages) {
        try {
            Write-Log "Installing $package..."
            $output = & pip install $package 2>&1
            if ($LASTEXITCODE -ne 0) {
                # Try with --user flag
                Write-Log "Retrying with --user flag..."
                $output = & pip install --user $package 2>&1
                if ($LASTEXITCODE -ne 0) {
                    throw "Failed to install $package"
                }
            }
            Write-Log "$package installed" -Level Success
        } catch {
            Write-Log "ERROR: Failed to install $package" -Level Error
            Write-Log $output
            throw
        }
    }
}

function Test-PythonModule {
    param(
        [string]$ModuleName
    )
    
    try {
        $output = & python -c "import $ModuleName; print('$ModuleName OK')" 2>&1
        return $LASTEXITCODE -eq 0
    } catch {
        return $false
    }
}

function Stop-ServiceSafely {
    param(
        [string]$ServiceName
    )
    
    Write-Log "Checking service status for: $ServiceName"
    $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    
    if (-not $service) {
        Write-Log "Service $ServiceName not found"
        return
    }
    
    Write-Log "Service $ServiceName found with status: $($service.Status)"
    
    if ($service.Status -eq "Running") {
        Write-Log "Stopping $ServiceName service..."
        try {
            Stop-Service -Name $ServiceName -Force -ErrorAction Stop
            Write-Log "Stop-Service command executed"
        } catch {
            Write-Log "ERROR: Stop-Service failed: $_" -Level Error
            return
        }
        
        # Wait for service to stop
        Write-Log "Waiting for service to stop (timeout: 30 seconds)..."
        $timeout = 30
        $elapsed = 0
        while (($service.Status -ne "Stopped") -and ($elapsed -lt $timeout)) {
            Start-Sleep -Seconds 1
            $elapsed++
            $service.Refresh()
            if ($elapsed % 5 -eq 0) {
                Write-Log "Still waiting... ($elapsed/$timeout seconds, status: $($service.Status))"
            }
        }
        
        if ($service.Status -ne "Stopped") {
            Write-Log "WARNING: Service did not stop within $timeout seconds (final status: $($service.Status))" -Level Warning
        } else {
            Write-Log "Service stopped successfully"
        }
    } elseif ($service.Status -eq "Stopped") {
        Write-Log "Service is already stopped"
    } else {
        Write-Log "Service status: $($service.Status)"
    }
}

function Remove-ServiceSafely {
    param(
        [string]$ServiceName
    )
    
    Write-Log "Attempting to remove service: $ServiceName"
    
    # PowerShell 6+ has Remove-Service cmdlet
    if (Get-Command Remove-Service -ErrorAction SilentlyContinue) {
        Write-Log "Using PowerShell Remove-Service cmdlet"
        try {
            Remove-Service -Name $ServiceName -ErrorAction Stop
            Write-Log "Service removed successfully using Remove-Service"
        } catch {
            Write-Log "ERROR: Remove-Service failed: $_" -Level Error
            Write-Log "Falling back to sc.exe"
            try {
                $output = & sc.exe delete $ServiceName 2>&1
                Write-Log "sc.exe delete output: $output"
            } catch {
                Write-Log "ERROR: sc.exe delete also failed: $_" -Level Error
            }
        }
    } else {
        Write-Log "Using sc.exe for service removal (older PowerShell)"
        try {
            $output = & sc.exe delete $ServiceName 2>&1
            Write-Log "sc.exe delete output: $output"
        } catch {
            Write-Log "ERROR: sc.exe delete failed: $_" -Level Error
        }
    }
    
    # Verify removal
    Start-Sleep -Seconds 2
    $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    if ($service) {
        Write-Log "WARNING: Service still exists after removal attempt" -Level Warning
    } else {
        Write-Log "Service removal verified - service no longer exists"
    }
}

# Functions are automatically available when dot-sourced