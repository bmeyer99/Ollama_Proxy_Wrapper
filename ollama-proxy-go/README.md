# Ollama Proxy - Go Edition

A lightweight, single-executable transparent proxy for Ollama that adds Prometheus metrics and analytics collection without requiring any changes to existing applications.

## Features

- **Single Executable**: No Python, no dependencies, just one ~10MB .exe file
- **Full Streaming Support**: Seamless handling of streaming responses
- **Prometheus Metrics**: Low-cardinality metrics at `/metrics`
- **Analytics Dashboard**: SQLite-backed analytics at `/analytics`
- **Windows Service**: Native Windows service support (no WinSW needed)
- **Drop-in Replacement**: Works with existing Ollama applications

## Quick Start

1. **Build the executable**:
   ```cmd
   build.bat
   ```

2. **Run the proxy**:
   ```cmd
   ollama-proxy.exe serve
   ```
   Or run a model:
   ```cmd
   ollama-proxy.exe run phi4
   ```

3. **Install as Windows Service** (optional):
   ```cmd
   install-service.bat   # Run as Administrator
   ```

## Architecture

```
User App → :11434 (Proxy) → :11435 (Ollama)
              ↓
         Metrics + Analytics
```

## Endpoints

- `http://localhost:11434` - Ollama API (proxied)
- `http://localhost:11434/metrics` - Prometheus metrics
- `http://localhost:11434/analytics` - Analytics dashboard
- `http://localhost:11434/analytics/stats` - Statistics API
- `http://localhost:11434/analytics/search` - Search API
- `http://localhost:11434/test` - Connectivity test

## Metrics Collected

### Prometheus Metrics (Low Cardinality)
- `ollama_request_duration_seconds` - Request duration histogram
- `ollama_tokens_generated` - Tokens generated histogram
- `ollama_tokens_per_second` - Token generation speed
- `ollama_requests_total` - Total requests counter
- `ollama_active_requests` - Currently active requests

### Analytics (High Detail)
- Full prompt text (truncated to 1000 chars)
- Response previews
- Token counts and generation speed
- Request duration and status codes
- Client IP and user agent
- Searchable SQLite database

## Building from Source

Requirements:
- Go 1.21 or later
- Windows, macOS, or Linux

```bash
# Download dependencies
go mod download

# Build for current platform
go build -o ollama-proxy .

# Build for Windows (from any platform)
GOOS=windows GOARCH=amd64 go build -o ollama-proxy.exe .
```

## Configuration

The proxy uses sensible defaults:
- Proxy Port: 11434 (standard Ollama port)
- Ollama Port: 11435 (hidden from users)
- Analytics Backend: SQLite
- Data Directory: `./ollama_analytics`
- Data Retention: 7 days

## Comparison with Python Version

| Feature | Python Version | Go Version |
|---------|---------------|------------|
| Executable Size | ~100MB (with deps) | ~10MB |
| Memory Usage | 50-100MB | 10-20MB |
| Startup Time | 2-5 seconds | <100ms |
| Dependencies | Python, aiohttp, etc. | None |
| Windows Service | Requires WinSW | Native support |
| Performance | Good | Excellent |

## Troubleshooting

### Port Already in Use
Stop any existing Ollama instances or change the proxy port.

### Service Won't Start
Check Windows Event Viewer for detailed error messages.

### Ollama Not Found
Ensure Ollama is installed and accessible in PATH or standard locations.

## License

Same as the original Ollama Proxy Wrapper project.