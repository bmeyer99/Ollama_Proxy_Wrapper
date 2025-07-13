# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a transparent metrics proxy for Ollama that adds Prometheus monitoring and analytics collection without requiring changes to existing applications. The proxy intercepts API calls, collects metrics, and forwards requests to Ollama.

## CRITICAL MISTAKES TO AVOID

### GO VERSION - STOP FUCKING UP
1. **The Go proxy already has NATIVE Windows service support via `golang.org/x/sys/windows/svc`**
   - DO NOT USE WinSW - it's unnecessary and causes problems
   - The Go executable runs with `-service` flag for service mode
   - Just use `sc.exe create` to install the service directly
   
2. **STOP CREATING NEW FILES**
   - Fix existing files, don't create test scripts
   - Don't create debug scripts
   - Don't create "helper" scripts
   - Just fix the fucking problem in the existing files

3. **The current issue**: sc.exe command line quoting
   - Service won't start because the binPath quoting is wrong
   - This is NOT a complex architectural issue
   - It's just getting the sc.exe syntax right

## Key Commands

### Installation and Setup
```bash
# Quick setup (Windows)
quick_install.bat

# Manual installation of dependencies
pip install aiohttp prometheus-client requests

# Windows Service Installation (runs on startup)
.\Install-Service.ps1         # PowerShell (Run as Administrator)

# WinSW-based service installation:
# - Downloads WinSW automatically
# - Uses clean Python runner (no pywin32 complexity)
# - Handles existing Ollama processes and auto-start
# - Saves settings for restoration on uninstall
```

### Running the Proxy
```bash
# Start with metrics collection
python ollama_wrapper.py serve

# Run a specific model with metrics
python ollama_wrapper.py run phi4

# Using convenience launchers
ollama_metrics.bat run phi4    # Windows batch
.\ollama.ps1 run phi4          # PowerShell

# Simple commands (bypass proxy)
python ollama_wrapper.py list
python ollama_wrapper.py ps

# Windows Service Mode (background, no terminal)
net start OllamaMetricsProxy   # Start service
net stop OllamaMetricsProxy    # Stop service
sc query OllamaMetricsProxy    # Check status
python ollama_runner.py        # Test in console mode
```

### Testing and Debugging
```bash
# Test setup
python ollama_wrapper.py --version

# Check connectivity and proxy status
curl http://localhost:11434/test

# View metrics
curl http://localhost:11434/metrics

# Check analytics stats
curl http://localhost:11434/analytics/stats

# Search analytics (SQLite backend only)
curl "http://localhost:11434/analytics/search?model=phi4"
curl "http://localhost:11434/analytics/search?prompt_search=summarize"
curl "http://localhost:11434/analytics/search?start_time=1640995200&end_time=1641081600"

# Test Ollama connectivity through the proxy
curl -X POST http://localhost:11434/api/tags

# Check if ports are available
netstat -an | findstr :11434
netstat -an | findstr :11435
```

## Architecture

### Core Components

1. **ollama_wrapper.py** - Main entry point that manages Ollama process lifecycle
   - Starts Ollama on port 11435 (hidden from users)
   - Launches metrics proxy on port 11434 (default Ollama port)
   - Handles command routing and process coordination

2. **ollama_fastapi_proxy.py** - FastAPI-based proxy server with dual collection
   - **Python 3.13 compatible**: Uses FastAPI + httpx instead of aiohttp
   - **Prometheus metrics**: Low-cardinality histograms for monitoring
   - **Analytics storage**: High-detail records for analysis
   - Supports multiple backends (JSONL, SQLite, Loki)

3. **PromptCategorizer** - Categorizes prompts to limit metric cardinality
   - Pattern-based classification (summarize, translate, code, etc.)
   - Prevents metric explosion from unique prompts
   - Max 50 categories to maintain Prometheus efficiency

4. **AnalyticsWriter** - Background analytics storage with async queue
   - Thread-safe writing to prevent blocking proxy
   - Configurable backends with automatic cleanup
   - Supports JSONL (compressed), SQLite (searchable), and Loki integration

5. **ollama_runner.py** - Clean Python runner for Windows service
   - Simple process management without Windows service framework complexity
   - Used by WinSW for reliable Windows service operation
   - Signal handling for graceful shutdown
   - Background logging to temp directory

6. **WinSW Integration** - Modern Windows service management
   - Uses WinSW (Windows Service Wrapper) instead of pywin32
   - Eliminates module import issues common with pywin32
   - Automatic download and configuration
   - Better service reliability and management

7. **OllamaManager.psm1** - PowerShell module for Ollama conflict resolution
   - Detects running Ollama processes and auto-start configurations
   - Safely stops Ollama and disables auto-start during installation
   - Backs up settings and restores them on uninstall
   - Handles Task Scheduler, Registry, Startup folder, and Service entries

### Request Flow
```
User App → :11434 (Proxy) → :11435 (Ollama)
              ↓
         Metrics + Analytics Collection
```

### Configuration Environment Variables
- `ANALYTICS_BACKEND`: Storage backend (jsonl, sqlite, loki)
- `ANALYTICS_DIR`: Storage directory (default: ./ollama_analytics)  
- `ANALYTICS_RETENTION_DAYS`: Data retention period (default: 7)
- `OLLAMA_HOST`: Set by wrapper to control Ollama server binding (internal use)

## Key Design Patterns

### Metrics Collection Strategy
- **Prometheus**: Aggregated metrics with limited cardinality for dashboards
- **Analytics**: Full-detail storage for debugging and analysis
- **Prompt categorization**: Prevents metric explosion from unique prompts

### Port Management
- **11434**: Proxy port (appears as standard Ollama to clients)
- **11435**: Actual Ollama port (hidden from users)
- Automatic port conflict detection and cleanup

### Error Handling
- Graceful fallback for missing dependencies
- Transparent passthrough for simple commands
- Comprehensive logging and error propagation

## Development Notes

### Adding New Metrics
1. Add Prometheus metric definition in `ollama_fastapi_proxy.py` (lines 36-79)
2. Update `record_interaction` method to populate the metric
3. Consider cardinality impact and use categorization if needed

### Analytics Backend Extension
1. Implement new backend in `AnalyticsWriter._write_record`
2. Add initialization logic in `AnalyticsWriter.__init__`
3. Update configuration validation

### Command Extensions
1. Add new commands to `PASSTHROUGH_COMMANDS` if they don't need metrics
2. Update command routing logic in `ollama_wrapper.py:main()`
3. Consider PowerShell wrapper updates for Windows integration

### Dependencies and Requirements
Dependencies are minimal and focused on core functionality:
- `aiohttp`: Async HTTP client/server for the proxy
- `prometheus-client`: Metrics collection and exposition
- `requests`: HTTP library for API calls
- `sqlite3`: Built-in Python module for analytics storage
- Standard library modules: `asyncio`, `json`, `time`, `pathlib`, etc.
- **WinSW**: Windows Service Wrapper (downloaded automatically)

Install dependencies:
```bash
pip install fastapi uvicorn httpx prometheus-client requests
```

### Troubleshooting Common Issues

#### Port Conflicts
- **Issue**: "Port 11434 is already in use"
- **Solution**: Stop existing Ollama instances or change ports in configuration
- **Check**: Use `netstat -an | findstr :11434` to see what's using the port

#### Python Import Errors
- **Issue**: "ImportError: No module named 'aiohttp'"
- **Solution**: Install dependencies with `pip install aiohttp prometheus-client requests`
- **Check**: Verify Python environment and PATH

#### Ollama Connection Issues
- **Issue**: "Ollama failed to start"
- **Solution**: Verify Ollama is installed and accessible
- **Check**: Test `ollama --version` directly

#### Existing Ollama Installation Conflicts
- **Issue**: Ollama already running or auto-starting at login
- **Solution**: The installer handles this automatically by:
  - Detecting and stopping running Ollama processes
  - Finding auto-start entries (Task Scheduler, Registry, Services)
  - Disabling them temporarily and backing up settings
  - Restoring original settings on uninstall
- **Manual Fix**: If needed, disable Ollama auto-start via Task Manager → Startup tab

#### Analytics Backend Issues
- **Issue**: SQLite search returns empty results
- **Solution**: Ensure `ANALYTICS_BACKEND=sqlite` is set before starting
- **Check**: Verify database file exists in `ANALYTICS_DIR`

#### WinSW Service Issues
- **Issue**: Service fails to start or install
- **Solution**: Check Windows Event Logs for detailed error messages
- **Check**: Verify Python is accessible from Windows services
- **Debug**: Run `python ollama_runner.py` manually to test functionality