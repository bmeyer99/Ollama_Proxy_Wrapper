<#
.SYNOPSIS
    Manages Ollama installation state for the metrics proxy
.DESCRIPTION
    Handles detection, stopping, and auto-start management of existing Ollama installations
#>

function Get-OllamaProcess {
    <#
    .SYNOPSIS
        Finds running Ollama processes
    #>
    $processes = @()
    
    # Check for ollama.exe processes
    $ollamaProcs = Get-Process -Name "ollama" -ErrorAction SilentlyContinue
    if ($ollamaProcs) {
        foreach ($proc in $ollamaProcs) {
            $processes += $proc
        }
    }
    
    # Check for processes using port 11434 (that aren't already ollama)
    try {
        $tcpConnections = Get-NetTCPConnection -LocalPort 11434 -ErrorAction SilentlyContinue
        foreach ($conn in $tcpConnections) {
            $proc = Get-Process -Id $conn.OwningProcess -ErrorAction SilentlyContinue
            if ($proc) {
                # Only add if it's not already in our list
                $alreadyAdded = $processes | Where-Object { $_.Id -eq $proc.Id }
                if (-not $alreadyAdded) {
                    $processes += $proc
                }
            }
        }
    } catch {
        # Get-NetTCPConnection might fail on some systems, that's OK
    }
    
    return $processes
}

function Stop-OllamaGracefully {
    <#
    .SYNOPSIS
        Stops Ollama processes gracefully
    #>
    param(
        [switch]$Force
    )
    
    Write-Log "Checking for running Ollama processes..."
    $processes = Get-OllamaProcess
    
    if ($processes.Count -eq 0) {
        Write-Log "No Ollama processes found"
        return $true
    }
    
    Write-Log "Found $($processes.Count) Ollama process(es)"
    
    # Log process details for debugging
    foreach ($proc in $processes) {
        Write-Log "Process: $($proc.Name) (PID: $($proc.Id), Path: $($proc.Path), User: $($proc.StartInfo.UserName))"
    }
    
    # Try graceful shutdown first
    foreach ($proc in $processes) {
        Write-Log "Attempting to stop $($proc.Name) (PID: $($proc.Id))..."
        try {
            # For console applications like Ollama, CloseMainWindow may not work
            # Try it first, but don't rely on it
            $gracefulStopped = $false
            
            if ($proc.MainWindowHandle -ne [System.IntPtr]::Zero) {
                Write-Log "Process has main window, trying CloseMainWindow..."
                $proc.CloseMainWindow() | Out-Null
                $gracefulStopped = $proc.WaitForExit(3000)
                Write-Log "CloseMainWindow result: $gracefulStopped"
            } else {
                Write-Log "Process has no main window (console app)"
            }
            
            if ($gracefulStopped) {
                Write-Log "Process stopped gracefully"
            } else {
                # Graceful didn't work, try force stop
                if ($Force) {
                    Write-Log "Graceful stop failed, attempting force termination..."
                    try {
                        Stop-Process -Id $proc.Id -Force -ErrorAction Stop
                        Write-Log "Stop-Process command executed for PID $($proc.Id)"
                        Start-Sleep -Seconds 2
                        
                        # Verify it's actually stopped
                        $stillRunning = Get-Process -Id $proc.Id -ErrorAction SilentlyContinue
                        if ($stillRunning) {
                            Write-Log "ERROR: Process $($proc.Id) still running after force stop attempt"
                            Write-Log "Process details: Name=$($stillRunning.Name), Status=$($stillRunning.Responding)"
                            return $false
                        } else {
                            Write-Log "SUCCESS: Process $($proc.Id) force stopped"
                        }
                    } catch {
                        Write-Log "ERROR: Stop-Process failed for PID $($proc.Id): $_"
                        Write-Log "Exception type: $($_.Exception.GetType().Name)"
                        Write-Log "Exception message: $($_.Exception.Message)"
                        return $false
                    }
                } else {
                    Write-Log "ERROR: Graceful stop failed and -Force not specified"
                    return $false
                }
            }
        } catch {
            Write-Log "ERROR: Exception during process stop attempt: $_"
            Write-Log "Exception type: $($_.Exception.GetType().Name)"
            if ($Force) {
                Write-Log "Attempting final force stop as fallback..."
                try {
                    Stop-Process -Id $proc.Id -Force -ErrorAction Stop
                    Write-Log "Final force stop succeeded"
                } catch {
                    Write-Log "ERROR: Final force stop failed: $_"
                    return $false
                }
            } else {
                return $false
            }
        }
    }
    
    # Verify all stopped
    Start-Sleep -Seconds 2
    Write-Log "Verifying all processes stopped..."
    $remaining = Get-OllamaProcess
    if ($remaining.Count -eq 0) {
        Write-Log "SUCCESS: All Ollama processes stopped successfully"
        return $true
    } else {
        Write-Log "ERROR: $($remaining.Count) processes still running:"
        foreach ($proc in $remaining) {
            Write-Log "Still running: $($proc.Name) (PID: $($proc.Id))"
        }
        return $false
    }
}

function Get-OllamaAutoStart {
    <#
    .SYNOPSIS
        Detects Ollama auto-start configurations
    .RETURNS
        Array of hashtables with Type, Path, and Details
    #>
    $autoStarts = @()
    
    # Check Task Scheduler
    try {
        $tasks = Get-ScheduledTask -TaskName "*ollama*" -ErrorAction SilentlyContinue
        foreach ($task in $tasks) {
            if ($task.State -eq "Ready" -or $task.State -eq "Running") {
                $autoStarts += @{
                    Type = "ScheduledTask"
                    Name = $task.TaskName
                    Path = $task.TaskPath
                    Enabled = $true
                    Details = $task.Description
                }
            }
        }
    } catch {}
    
    # Check Registry Run keys (Current User)
    $runKeys = @(
        "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run",
        "HKCU:\Software\Microsoft\Windows\CurrentVersion\RunOnce"
    )
    
    foreach ($key in $runKeys) {
        if (Test-Path $key) {
            $items = Get-ItemProperty -Path $key -ErrorAction SilentlyContinue
            $items.PSObject.Properties | Where-Object { 
                $_.Name -notlike "PS*" -and $_.Value -like "*ollama*" 
            } | ForEach-Object {
                $autoStarts += @{
                    Type = "Registry"
                    Name = $_.Name
                    Path = $key
                    Value = $_.Value
                    Enabled = $true
                }
            }
        }
    }
    
    # Check Startup folder
    $startupPaths = @(
        [Environment]::GetFolderPath("Startup"),
        "$env:ProgramData\Microsoft\Windows\Start Menu\Programs\Startup"
    )
    
    foreach ($path in $startupPaths) {
        if (Test-Path $path) {
            Get-ChildItem -Path $path -Filter "*ollama*" -ErrorAction SilentlyContinue | Where-Object { -not $_.Name.StartsWith("DISABLED_") } | ForEach-Object {
                $autoStarts += @{
                    Type = "StartupFolder"
                    Name = $_.Name
                    Path = $_.FullName
                    Enabled = $true
                }
            }
        }
    }
    
    # Check Windows Services (exclude our own metrics proxy service)
    $services = Get-Service -Name "*ollama*" -ErrorAction SilentlyContinue | Where-Object { $_.Name -ne "OllamaMetricsProxy" }
    foreach ($service in $services) {
        if ($service.StartType -eq "Automatic") {
            $autoStarts += @{
                Type = "Service"
                Name = $service.Name
                DisplayName = $service.DisplayName
                StartType = $service.StartType
                Status = $service.Status
                Enabled = $true
            }
        }
    }
    
    return $autoStarts
}

function Disable-OllamaAutoStart {
    <#
    .SYNOPSIS
        Disables Ollama auto-start configurations
    .DESCRIPTION
        Saves the current state to a JSON file for restoration
    #>
    param(
        [string]$BackupPath = "$env:TEMP\ollama_autostart_backup.json"
    )
    
    Write-Host "`nChecking for Ollama auto-start configurations..." -ForegroundColor Cyan
    $autoStarts = Get-OllamaAutoStart
    
    if ($autoStarts.Count -eq 0) {
        Write-Host "No auto-start configurations found" -ForegroundColor Green
        return $true
    }
    
    Write-Host "Found $($autoStarts.Count) auto-start configuration(s):" -ForegroundColor Yellow
    
    # Save backup
    $autoStarts | ConvertTo-Json -Depth 10 | Out-File -FilePath $BackupPath -Force
    Write-Host "Backup saved to: $BackupPath" -ForegroundColor Gray
    
    # Disable each auto-start
    foreach ($config in $autoStarts) {
        Write-Host "`n  Disabling: $($config.Name)" -ForegroundColor Yellow
        Write-Host "  Type: $($config.Type)" -ForegroundColor Gray
        
        try {
            switch ($config.Type) {
                "ScheduledTask" {
                    Disable-ScheduledTask -TaskName $config.Name -ErrorAction Stop | Out-Null
                    Write-Host "  Scheduled task disabled" -ForegroundColor Green
                }
                
                "Registry" {
                    # Rename the registry value (prefix with DISABLED_)
                    $regPath = Get-Item -Path $config.Path
                    $newName = "DISABLED_OLLAMA_$($config.Name)"
                    $regPath | New-ItemProperty -Name $newName -Value $config.Value -Force | Out-Null
                    $regPath | Remove-ItemProperty -Name $config.Name -Force
                    Write-Host "  Registry entry disabled (renamed to $newName)" -ForegroundColor Green
                }
                
                "StartupFolder" {
                    # Check if already disabled
                    $fileName = [System.IO.Path]::GetFileName($config.Path)
                    if ($fileName.StartsWith("DISABLED_")) {
                        Write-Host "  Startup item already disabled: $fileName" -ForegroundColor Yellow
                    } else {
                        # Rename the file
                        $newName = "DISABLED_$fileName"
                        $newPath = Join-Path (Split-Path $config.Path) $newName
                        Rename-Item -Path $config.Path -NewName $newName -Force
                        Write-Host "  Startup item disabled (renamed to $newName)" -ForegroundColor Green
                    }
                }
                
                "Service" {
                    Set-Service -Name $config.Name -StartupType Manual -ErrorAction Stop
                    Write-Host "  Service startup type changed to Manual" -ForegroundColor Green
                }
            }
        } catch {
            Write-Host "  Failed to disable: $_" -ForegroundColor Red
            return $false
        }
    }
    
    Write-Host "`nAll auto-start configurations have been disabled" -ForegroundColor Green
    Write-Host "Note: These will be restored when you uninstall the metrics proxy" -ForegroundColor Gray
    return $true
}

function Restore-OllamaAutoStart {
    <#
    .SYNOPSIS
        Restores Ollama auto-start configurations from backup
    #>
    param(
        [string]$BackupPath = "$env:TEMP\ollama_autostart_backup.json"
    )
    
    if (-not (Test-Path $BackupPath)) {
        Write-Host "No auto-start backup found. Nothing to restore." -ForegroundColor Gray
        return $true
    }
    
    Write-Host "`nRestoring Ollama auto-start configurations..." -ForegroundColor Cyan
    
    try {
        $autoStarts = Get-Content -Path $BackupPath | ConvertFrom-Json
        
        foreach ($config in $autoStarts) {
            Write-Host "  Restoring: $($config.Name)" -ForegroundColor Yellow
            
            try {
                switch ($config.Type) {
                    "ScheduledTask" {
                        Enable-ScheduledTask -TaskName $config.Name -ErrorAction Stop | Out-Null
                        Write-Host "  Scheduled task enabled" -ForegroundColor Green
                    }
                    
                    "Registry" {
                        $regPath = Get-Item -Path $config.Path
                        # Look for our disabled entry
                        $disabledName = "DISABLED_OLLAMA_$($config.Name)"
                        if ($regPath.GetValue($disabledName)) {
                            $regPath | New-ItemProperty -Name $config.Name -Value $config.Value -Force | Out-Null
                            $regPath | Remove-ItemProperty -Name $disabledName -Force
                            Write-Host "  Registry entry restored" -ForegroundColor Green
                        }
                    }
                    
                    "StartupFolder" {
                        $dir = Split-Path $config.Path
                        $disabledPath = Join-Path $dir "DISABLED_$($config.Name)"
                        if (Test-Path $disabledPath) {
                            Rename-Item -Path $disabledPath -NewName $config.Name -Force
                            Write-Host "  Startup item restored" -ForegroundColor Green
                        }
                    }
                    
                    "Service" {
                        Set-Service -Name $config.Name -StartupType Automatic -ErrorAction Stop
                        Write-Host "  Service startup type restored to Automatic" -ForegroundColor Green
                    }
                }
            } catch {
                Write-Host "  Failed to restore: $_" -ForegroundColor Yellow
            }
        }
        
        # Remove backup file
        Remove-Item -Path $BackupPath -Force -ErrorAction SilentlyContinue
        Write-Host "`nAuto-start configurations restored" -ForegroundColor Green
        
    } catch {
        Write-Host "Failed to restore auto-start configurations: $_" -ForegroundColor Red
        return $false
    }
    
    return $true
}

function Test-OllamaInstallation {
    <#
    .SYNOPSIS
        Verifies Ollama is installed and accessible
    #>
    try {
        $ollamaPath = (Get-Command ollama -ErrorAction Stop).Source
        $version = & ollama --version 2>&1
        
        return @{
            Installed = $true
            Path = $ollamaPath
            Version = $version
        }
    } catch {
        # Check common installation paths
        $commonPaths = @(
            "$env:LOCALAPPDATA\Programs\Ollama\ollama.exe",
            "$env:ProgramFiles\Ollama\ollama.exe",
            "C:\Program Files\Ollama\ollama.exe"
        )
        
        foreach ($path in $commonPaths) {
            if (Test-Path $path) {
                try {
                    $version = & $path --version 2>&1
                    return @{
                        Installed = $true
                        Path = $path
                        Version = $version
                        NotInPath = $true
                    }
                } catch {}
            }
        }
        
        return @{
            Installed = $false
            Path = $null
            Version = $null
        }
    }
}

# Export functions (required for .psm1 modules)
Export-ModuleMember -Function @(
    'Get-OllamaProcess',
    'Stop-OllamaGracefully', 
    'Get-OllamaAutoStart',
    'Disable-OllamaAutoStart',
    'Restore-OllamaAutoStart',
    'Test-OllamaInstallation'
)