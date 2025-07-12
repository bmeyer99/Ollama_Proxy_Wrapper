# Transparent Ollama Wrapper with Metrics
# Usage: .\ollama.ps1 [any ollama command]
# Examples:
#   .\ollama.ps1 list
#   .\ollama.ps1 run phi4
#   .\ollama.ps1 serve

param(
    [Parameter(Position=0, ValueFromRemainingArguments=$true)]
    [string[]]$OllamaArgs
)

$OLLAMA_REAL_PORT = 11435
$PROXY_PORT = 11434
$SCRIPT_DIR = Split-Path -Parent $MyInvocation.MyCommand.Path

# Simple commands that don't need proxy
$PASSTHROUGH_COMMANDS = @('list', 'ps', 'rm', 'cp', 'help', '--version', '-v')

function Test-Port {
    param($Port)
    try {
        $connection = New-Object System.Net.Sockets.TcpClient
        $connection.Connect("localhost", $Port)
        $connection.Close()
        return $true
    } catch {
        return $false
    }
}

function Find-Ollama {
    # Try to find ollama.exe
    $ollamaPath = (Get-Command ollama.exe -ErrorAction SilentlyContinue).Path
    if ($ollamaPath) { return $ollamaPath }
    
    # Check common locations
    $paths = @(
        "$env:LOCALAPPDATA\Programs\Ollama\ollama.exe",
        "$env:ProgramFiles\Ollama\ollama.exe",
        "C:\ollama\ollama.exe"
    )
    
    foreach ($path in $paths) {
        if (Test-Path $path) { return $path }
    }
    
    return "ollama.exe"  # Hope it's in PATH
}

# Check command
$command = if ($OllamaArgs.Count -gt 0) { $OllamaArgs[0] } else { "serve" }

# Passthrough for simple commands
if ($command -in $PASSTHROUGH_COMMANDS) {
    & (Find-Ollama) $OllamaArgs
    exit $LASTEXITCODE
}

# Check for Python
try {
    python --version | Out-Null
} catch {
    Write-Host "Warning: Python not found - running without metrics" -ForegroundColor Yellow
    Write-Host "Install Python from python.org to enable metrics" -ForegroundColor Yellow
    & (Find-Ollama) $OllamaArgs
    exit $LASTEXITCODE
}

# Check for wrapper script
$wrapperPath = Join-Path $SCRIPT_DIR "ollama_wrapper.py"
if (-not (Test-Path $wrapperPath)) {
    Write-Host "Error: ollama_wrapper.py not found" -ForegroundColor Red
    Write-Host "Running without metrics..." -ForegroundColor Yellow
    & (Find-Ollama) $OllamaArgs
    exit $LASTEXITCODE
}

# Check for FastAPI proxy script
$proxyPath = Join-Path $SCRIPT_DIR "ollama_fastapi_proxy.py"
if (-not (Test-Path $proxyPath)) {
    Write-Host "Error: ollama_fastapi_proxy.py not found" -ForegroundColor Red
    Write-Host "Please ensure all files are in the same directory" -ForegroundColor Yellow
    exit 1
}

# Check ports
if (Test-Port $PROXY_PORT) {
    Write-Host "Error: Port $PROXY_PORT is already in use" -ForegroundColor Red
    Write-Host "Is Ollama already running? Stop it first." -ForegroundColor Yellow
    exit 1
}

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "   Ollama with Transparent Metrics" -ForegroundColor Cyan  
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Starting Ollama with metrics collection..." -ForegroundColor Green
Write-Host "Your apps connect to: http://localhost:$PROXY_PORT (as usual)" -ForegroundColor White
Write-Host "Metrics available at: http://localhost:$PROXY_PORT/metrics" -ForegroundColor White
Write-Host ""

# Run the Python wrapper
python $wrapperPath $OllamaArgs
exit $LASTEXITCODE