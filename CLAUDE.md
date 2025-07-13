# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the **Ollama Metrics Proxy** - a transparent HTTP reverse proxy for Ollama that adds Prometheus metrics collection and analytics without requiring changes to existing applications. The proxy intercepts all API calls to Ollama and collects detailed metrics while forwarding requests seamlessly.

**Key Architecture:**
- Proxy listens on port 11434 (default Ollama port) 
- Ollama runs on port 11435 (hidden from users)
- All requests flow: `Client → :11434 (Proxy) → :11435 (Ollama)`
- Metrics collected via Prometheus + SQLite analytics storage

## Common Development Commands

# Windows batch build script
./build.bat

# Quick build (Windows)
./quick-build.bat
```

### Testing
```bash
# Test basic functionality
ollama-proxy.exe serve

# Test proxy connectivity
curl http://localhost:11434/test

# Test metrics endpoint
curl http://localhost:11434/metrics

# Test analytics API
curl http://localhost:11434/analytics/stats
```

### Service Management (Windows)
```powershell
# Install as Windows service (requires admin)
./Install-Service.ps1

# Install via launcher (auto-elevates)
./install-service-launcher.bat

# Uninstall service
./Uninstall-Service.ps1

# Manual service commands
sc start OllamaMetricsProxy
sc stop OllamaMetricsProxy
```

## Core Architecture

### Main Components
- **main.go**: CLI entry point, handles command routing and port management
- **proxy.go**: HTTP reverse proxy with metrics collection and streaming support
- **metrics.go**: Prometheus metrics definitions and categorization logic
- **analytics.go**: SQLite analytics storage with search/export capabilities
- **ollama.go**: Ollama process management and executable discovery
- **service.go**: Windows service implementation with proper logging
- **context.go**: Request context for metrics correlation across streaming responses

### Service vs Console Mode
The application automatically detects execution context:
- **Console mode**: Direct execution via `ollama-proxy.exe serve`
- **Service mode**: Background Windows service via `-service` flag
- Service mode uses file logging, console mode uses stdout

### Platform-Specific Files
- **logging_windows.go**: Windows-specific file logging for service mode
- **logging_other.go**: Unix/console logging fallback
- **ollama_windows.go**: Windows process management (taskkill, etc.)
- **ollama_other.go**: Unix process management fallback

## Key Development Patterns

### Metrics Collection
- Prometheus metrics use controlled cardinality via `PromptCategorizer`
- Analytics records are comprehensive with full request/response context
- Streaming responses are handled via `streamingResponseBody` wrapper
- Metrics recorded at request completion, not during streaming

### Error Handling
- Service mode errors logged to files + Windows Event Log
- Console mode errors to stdout with user-friendly messages
- Graceful degradation when Ollama unavailable
- Port conflict detection with clear error messages

### Configuration
Environment variables for service behavior:
- `OLLAMA_HOST`: Backend Ollama address (default: 0.0.0.0:11435)
- `ANALYTICS_BACKEND`: Storage type (sqlite/jsonl/none)
- `ANALYTICS_DIR`: Analytics storage directory
- `ANALYTICS_RETENTION_DAYS`: Data retention period

### Cross-Platform Considerations
- Build tags separate Windows service code (`//go:build windows`)
- Service detection via executable path and interactive session checks
- Windows-specific process management for reliable Ollama lifecycle

## Testing Guidelines

### Manual Testing Flow
1. Build the executable
2. Verify Ollama is installed and accessible
3. Test console mode: `ollama-proxy.exe serve`
4. Verify proxy responds on port 11434
5. Test metrics endpoint returns Prometheus format
6. Test analytics endpoints return valid JSON
7. For service mode, test installation and service lifecycle

### Common Issues to Test
- Port 11434 already in use (existing Ollama)
- Ollama not found in PATH
- Service startup failures (check Windows Event Log)
- Streaming response handling
- Analytics database corruption recovery

## Analytics Schema

The SQLite analytics table (`interactions`) stores:
- Request metadata (model, endpoint, client IP)
- Performance metrics (duration, tokens/sec, load time)
- Content samples (prompt preview, response preview)
- Error tracking and status codes

### Key Analytics Endpoints
- `/analytics` - Web dashboard
- `/analytics/stats` - Basic statistics API
- `/analytics/search` - Query interface with filters
- `/analytics/export` - JSON/CSV export functionality

## Security Considerations

- Service runs as LocalSystem for Ollama management privileges
- Analytics data stored locally (no network transmission)
- Content previews are truncated to prevent storage bloat
- Client IP tracking for usage analysis (not authentication)

## Build Dependencies

### Runtime Dependencies
- None (single executable)

### Build Dependencies  
- Go 1.21+
- `github.com/prometheus/client_golang` - Metrics
- `golang.org/x/sys/windows` - Service support (Windows only)
- `modernc.org/sqlite` - Analytics storage

## Development Tips

- Use `LogPrintf()` for service-compatible logging
- Test both console and service modes for any proxy changes
- Streaming response changes require testing with actual Ollama models
- Analytics schema changes need migration logic in `initSQLite()`
- Prometheus metric changes should consider cardinality impact