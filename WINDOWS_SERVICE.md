# Windows Service Installation Guide

This guide explains how to run the Ollama Metrics Proxy as a Windows service that starts automatically on boot.

## Quick Start

### Option 1: PowerShell Scripts (Recommended)
1. **Install**: Right-click `Install-Service.ps1` → Run with PowerShell (as administrator)
2. **Uninstall**: Right-click `Uninstall-Service.ps1` → Run with PowerShell (as administrator)
3. **Test**: Run `service_manager.ps1 test` from PowerShell

### Option 2: Service Manager Script (Advanced)
```powershell
# Install and start service
.\service_manager.ps1 install

# Check status
.\service_manager.ps1 status

# Test in console mode
.\service_manager.ps1 test

# Uninstall
.\service_manager.ps1 uninstall
```

## Features

### ✅ Windows Service
- Runs automatically on startup
- No terminal window required
- Managed through Windows Services console

### ✅ System Tray Icon
- Shows service status: 🦙🔒 (Ollama behind bars)
- Click to show/hide status window
- Stop service from tray menu

### ✅ Monitoring Endpoints
- **Proxy**: `http://localhost:11434` (appears as normal Ollama)
- **Metrics**: `http://localhost:11434/metrics` (Prometheus)
- **Analytics**: `http://localhost:11434/analytics/stats`

## Requirements

- **Python 3.7+** with pip
- **Ollama** installed and in PATH
- **Administrator privileges** for service installation

Required Python packages (installed automatically):
- `pywin32` - Windows service support
- `aiohttp` - HTTP proxy server  
- `prometheus-client` - Metrics collection

## Service Details

- **Service Name**: `OllamaMetricsProxy`
- **Display Name**: `Ollama Metrics Proxy Service`
- **Startup Type**: Automatic
- **Log File**: `%TEMP%\ollama_service\ollama_service.log`

## Port Configuration

- **11434**: Proxy port (your apps connect here)
- **11435**: Ollama server port (hidden from apps)

## Troubleshooting

### Service Won't Install
```bash
# Check Python is available
python --version

# Install dependencies manually
pip install pywin32 aiohttp prometheus-client

# Try installing with verbose output
python ollama_service.py install
```

### Service Won't Start
1. Check log file: `%TEMP%\ollama_service\ollama_service.log`
2. Verify Ollama is installed: `ollama --version`
3. Check port conflicts: `netstat -an | findstr :11434`
4. Test in console mode: `python ollama_service.py`

### System Tray Not Showing
- Tray icon uses tkinter (included with Python)
- Check Windows notification area settings
- Look for the 🦙🔒 icon in system tray

### Port Already in Use
```bash
# Find what's using the port
netstat -ano | findstr :11434

# Stop existing Ollama
taskkill /F /IM ollama.exe

# Or change ports in ollama_service.py
```

## Manual Service Management

Using Windows Services console:
1. Press `Win+R`, type `services.msc`
2. Find "Ollama Metrics Proxy Service"
3. Right-click → Start/Stop/Properties

Using command line:
```bash
# Start service
sc start OllamaMetricsProxy

# Stop service  
sc stop OllamaMetricsProxy

# Remove service
sc delete OllamaMetricsProxy
```

## Testing Without Installation

Run in console mode for testing:
```bash
python ollama_service.py
```

This starts the service in the foreground with a system tray icon, perfect for testing before installing as a Windows service.

## Uninstalling

### Complete Removal
1. Run `Uninstall-Service.ps1` as administrator
   - Or use: `.\service_manager.ps1 uninstall`
2. Optionally remove Python packages:
   ```bash
   pip uninstall pywin32 aiohttp prometheus-client
   ```

The service files can remain in the directory for future use.