# Ollama Metrics Proxy

A transparent HTTP proxy for Ollama that adds Prometheus metrics collection and analytics without requiring any changes to your existing applications.

## Features

- **Transparent Proxying**: Acts as a drop-in replacement for Ollama on the default port (11434)
- **Prometheus Metrics**: Exposes detailed metrics about model usage, request duration, and token generation
- **Analytics Storage**: SQLite-based analytics with WAL mode for high-performance concurrent access
- **Rate Limiting**: Automatic backpressure with configurable concurrent request limits
- **Graceful Shutdown**: Ensures in-flight requests complete before shutdown
- **Windows Service**: Can run as a Windows service for automatic startup
- **Structured Logging**: Built-in slog integration for better observability
- **Configurable Ports**: Environment variable support for flexible deployment
- **Zero Configuration**: Works out of the box with sensible defaults
- **Single Executable**: No dependencies required - just one executable file

## Recent Improvements

### Reliability

- **Automatic Crash Recovery**: Health monitoring checks Ollama every 30 seconds and auto-restarts if crashed
- **SQLite Connection Pool**: Configured for single-writer mode with WAL journal for optimal performance
- **Graceful Shutdown**: 10-second grace period ensures in-flight requests complete before shutdown
- **Memory Leak Fixes**: Proper cleanup of streaming response bodies on client disconnect
- **Context Cancellation**: Stops processing when clients disconnect to avoid wasted work

### Performance

- **Optimized Analytics Queries**: SQL aggregations replace in-memory processing for better performance
- **Rate Limiting**: Semaphore-based limiting (50 concurrent requests) prevents backend overload
- **Cardinality Control**: Removed unbounded labels from Prometheus metrics

### Observability

- **Structured Logging**: Built-in `slog` integration with structured fields
- **Prompt Categorization**: Automatic categorization of prompts for better metrics grouping

## Quick Start

### Build from Source

```bash
# Build the executable
go build -o ollama-proxy.exe .

# Or use the build script
.\build.bat
```

### Run as Console Application

```bash
# Start the proxy (runs Ollama on 11435, proxy on 11434)
ollama-proxy.exe serve

# Use Ollama normally - all requests go through the proxy
ollama run phi4
```

### Install as Windows Service

```powershell
# Run as Administrator
.\Install-Service.ps1

# Or use the launcher (auto-elevates)
.\install-service-launcher.bat
```

## Architecture

The proxy works by:
1. Starting Ollama on port 11435 (hidden from users)
2. Listening on port 11434 (the default Ollama port)
3. Forwarding all requests while collecting metrics
4. Storing detailed analytics in SQLite

```
Your App → :11434 (Proxy) → :11435 (Ollama)
              ↓
         Metrics + Analytics
```

## Metrics

Access Prometheus metrics at: `http://localhost:11434/metrics`

Available metrics:

- `ollama_requests_total` - Total requests by model, endpoint, prompt_category, and status
- `ollama_request_duration_seconds` - Request duration histogram by model, endpoint, and prompt_category
- `ollama_tokens_generated` - Token generation distribution by model and prompt_category
- `ollama_tokens_per_second` - Token generation speed by model and prompt_category
- `ollama_active_requests` - Currently active requests

**Note**: Client IP is tracked in SQLite analytics but not in Prometheus metrics to prevent cardinality explosion.

## Analytics

The proxy stores detailed analytics in SQLite for each request:

- Model used
- Prompt and response preview
- Token counts and generation speed
- Request duration and status
- Client IP and user agent

### Analytics Endpoints

- `http://localhost:11434/analytics` - Web dashboard
- `http://localhost:11434/analytics/stats` - Statistics API
- `http://localhost:11434/analytics/search` - Search API
- `http://localhost:11434/analytics/export` - Export data as JSON/CSV

### Search Examples

```bash
# Search by model
curl "http://localhost:11434/analytics/search?model=phi4"

# Search by prompt content
curl "http://localhost:11434/analytics/search?prompt_search=summarize"

# Search by time range (Unix timestamps)
curl "http://localhost:11434/analytics/search?start_time=1640995200&end_time=1641081600"

# Limit results
curl "http://localhost:11434/analytics/search?limit=50"
```

## Configuration

### Environment Variables

**Port Configuration**:

- `PROXY_PORT` - Proxy frontend port where apps connect (default: `11434`)
- `OLLAMA_BACKEND_PORT` - Backend Ollama port (default: `11435`)
- `OLLAMA_HOST` - Ollama bind address (default: `0.0.0.0:11435`)

**Analytics Configuration**:

- `ANALYTICS_BACKEND` - Storage backend: `sqlite` (default), `jsonl`, or `none`
- `ANALYTICS_DIR` - Analytics storage directory (default: `./ollama_analytics`)
- `ANALYTICS_RETENTION_DAYS` - Days to keep analytics (default: 7)

**Performance Tuning**:

The proxy includes automatic rate limiting (50 concurrent requests) and graceful shutdown with a 10-second grace period for in-flight requests.

### Service Configuration

When running as a Windows service:
- Logs are stored in `C:\ProgramData\OllamaProxy\logs\`
- Analytics are stored in `C:\ProgramData\OllamaProxy\analytics\`
- Service runs as LocalSystem with delayed auto-start

## Grafana Integration

The project includes a pre-built Grafana dashboard (`../Grafana/Provisioning/Dashboards/grafana_ollama_dashboard.json`) with:

- Request rate by model (5-minute intervals)
- Request duration percentiles
- Token generation metrics
- Error rates by model
- Client IP breakdown
- Model usage distribution

Import the dashboard into Grafana and configure it to use your Prometheus data source.

## Troubleshooting

### Port Already in Use

If port 11434 is already in use:

1. Stop any existing Ollama instances
2. Check with: `netstat -an | findstr :11434`
3. Or configure different ports:

   ```bash
   set PROXY_PORT=8080
   set OLLAMA_BACKEND_PORT=8081
   ollama-proxy.exe serve
   ```

### Service Won't Start

1. Check Windows Event Viewer for errors
2. Look for logs in `C:\ProgramData\OllamaProxy\logs\`
3. Verify Ollama is installed and accessible
4. Run `ollama-proxy.exe serve` manually to see errors

### No Metrics Appearing

1. Verify the proxy is running: `curl http://localhost:11434/test`
2. Check if Ollama is responding: `curl http://localhost:11435/api/tags`
3. Ensure your applications are connecting to port 11434

### Service Doesn't Stop Ollama

If Ollama processes remain after stopping the service:

1. Check Windows Event Viewer for termination errors
2. Manually kill with: `taskkill /F /IM ollama.exe`
3. Restart the service

### Ollama Keeps Crashing Under Load

The service includes automatic crash recovery:

- Health checks run every 30 seconds
- Ollama automatically restarts after 3 failed health checks (90 seconds)
- Check logs in `C:\ProgramData\OllamaProxy\logs\` for crash details
- If crashes persist, consider:
  - Reducing concurrent request limits
  - Increasing system resources (RAM/GPU)
  - Checking Ollama logs for memory issues

## Development

### Project Structure

```
ollama-proxy-go/
├── main.go                 # Entry point and CLI handling
├── proxy.go               # HTTP reverse proxy implementation
├── metrics.go             # Prometheus metrics collection
├── analytics.go           # Analytics storage and querying
├── ollama.go              # Ollama process management
├── service.go             # Windows service implementation
├── context.go             # Request context for metrics
├── logging.go             # Service-mode file logging
└── analytics_dashboard.html # Web UI for analytics
```

### Building

Requirements:
- Go 1.21 or later
- Windows for service functionality

```bash
# Standard build
go build -o ollama-proxy.exe .

# Optimized build (smaller binary)
go build -ldflags="-w -s" -o ollama-proxy.exe .
```

### Adding New Metrics

1. Define the metric in `metrics.go`
2. Update `recordMetrics()` in `proxy.go` to populate it
3. Consider cardinality - use categorization for high-cardinality labels

### Testing

```bash
# Test basic functionality
ollama-proxy.exe serve

# In another terminal
curl http://localhost:11434/test
curl http://localhost:11434/metrics
curl http://localhost:11434/analytics/stats
```

## Performance

- **Memory Usage**: ~10-20MB
- **CPU Usage**: Minimal overhead (~1-2% additional)
- **Latency**: <1ms additional latency for requests
- **Throughput**: Supports full Ollama streaming performance

## Dependencies

Runtime: None (single executable)

Build-time:
- `github.com/prometheus/client_golang` - Metrics collection
- `golang.org/x/sys/windows` - Windows service support
- `modernc.org/sqlite` - SQLite database

## License

MIT License - see parent project for details