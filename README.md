# Ollama Transparent Metrics Proxy

A transparent proxy that adds Prometheus metrics and analytics to Ollama without requiring any changes to existing applications.

## What It Does

- Intercepts all Ollama API calls to collect metrics
- Exposes Prometheus metrics for monitoring (latency, tokens/sec, error rates)
- Stores detailed analytics for every request (prompts, responses, timings)
- Works transparently - your apps still connect to `localhost:11434`

## Quick Start

### Option 1: Run as Windows Service (Recommended)
1. Clone this repository
2. Right-click `install_service.bat` → Run as administrator
3. Service starts automatically with Windows boot
4. Look for 🦙🔒 icon in system tray

### Option 2: Manual Start
1. Clone this repository
2. Install Python dependencies:
   ```bash
   pip install aiohttp prometheus-client
   ```
3. Run the installer:
   ```bash
   quick_install.bat
   ```
4. Use Ollama with metrics:
   ```bash
   ollama_metrics.bat run phi4
   ```

Metrics are now available at:
- `http://localhost:11434/metrics` - Prometheus metrics
- `http://localhost:11434/analytics/search` - Query analytics

## How It Works

The wrapper starts Ollama on port 11435 and a metrics proxy on port 11434 (the default Ollama port). All requests flow through the proxy, which collects metrics before forwarding to Ollama.

```
Your App → :11434 (Proxy) → :11435 (Ollama)
              ↓
         Metrics Collection
```

## Project Structure

- **`ollama_hybrid_proxy.py`** - Async metrics proxy with dual collection (Prometheus + Analytics)
- **`ollama_wrapper.py`** - Main entry point that manages Ollama process lifecycle
- **`ollama_service.py`** - Windows service wrapper with system tray icon
- **`quick_install.bat`** - Automated Windows installer with dependency check
- **`install_service.bat`** - Install as Windows service (run as admin)
- **`ollama_metrics.bat`** - Simplified Windows launcher
- **`ollama.ps1`** - Advanced PowerShell wrapper with enhanced features
- **`service_manager.ps1`** - PowerShell service management tool
- **`CLAUDE.md`** - Comprehensive development and architecture guide
- **`WINDOWS_SERVICE.md`** - Windows service installation guide

## Metrics Collected

### Prometheus Metrics (Low Cardinality)
- `ollama_requests_total` - Request count by model, endpoint, status, prompt category
- `ollama_request_duration_seconds` - Latency distribution histogram
- `ollama_tokens_generated` - Token generation count histogram
- `ollama_tokens_per_second` - Generation speed histogram
- `ollama_active_requests` - Currently processing requests gauge
- `ollama_analytics_queue_size` - Analytics write queue depth
- `ollama_analytics_writes_total` - Analytics write success/error counter

### Analytics Storage (High Detail)
- **Full prompt text** with categorization
- **Detailed timings** (eval, load, total duration)
- **Token metrics** (prompt tokens, generated tokens, tokens/sec)
- **Request metadata** (client IP, user agent, interaction ID)
- **Error tracking** with full stack traces
- **Multiple backends**: JSONL (compressed), SQLite (searchable), Loki (planned)
- **Automatic cleanup** based on retention policy

## Advanced Features

### Multiple Launch Options

**Windows Service (Background)**:
```powershell
# Install and start service (run as admin)
.\service_manager.ps1 install

# Service management
.\service_manager.ps1 status   # Check status
.\service_manager.ps1 start    # Start service
.\service_manager.ps1 stop     # Stop service
.\service_manager.ps1 test     # Test in console mode
```

**Windows Batch (Simple)**:
```cmd
ollama_metrics.bat run phi4
ollama_metrics.bat serve
```

**PowerShell (Advanced)**:
```powershell
.\ollama.ps1 run phi4        # Start with metrics
.\ollama.ps1 list            # List models (passthrough)
.\ollama.ps1 serve           # Start server with metrics
```

**Direct Python**:
```bash
python ollama_wrapper.py run phi4
python ollama_wrapper.py serve
```

### Analytics Backends

Configure storage backend via environment variable:

```bash
# Compressed JSONL files (default) - efficient for log aggregation
set ANALYTICS_BACKEND=jsonl

# SQLite database - enables search API and complex queries
set ANALYTICS_BACKEND=sqlite

# Loki integration - for centralized log aggregation (planned)
set ANALYTICS_BACKEND=loki
```

**Backend Comparison**:
- **JSONL**: Fastest writes, great for log shipping, compressed storage
- **SQLite**: Searchable, queryable, best for analysis and debugging
- **Loki**: Centralized logging, good for multi-instance deployments

### Analytics API (SQLite Backend)

Query historical interaction data:

```bash
# Get analytics statistics
curl http://localhost:11434/analytics/stats

# Search by time range
curl "http://localhost:11434/analytics/search?start_time=1640995200&end_time=1641081600"

# Search by prompt content
curl "http://localhost:11434/analytics/search?prompt_search=summarize"

# Filter by model
curl "http://localhost:11434/analytics/search?model=phi4"

# Find interactions by category
curl "http://localhost:11434/analytics/search?prompt_category=code_write"
```

**Available Search Parameters**:
- `start_time`, `end_time` - Unix timestamps
- `model` - Exact model name match
- `prompt_search` - Text search in prompt content
- `prompt_category` - Categorized prompt types
- Results limited to 100 records, ordered by timestamp DESC

### Configuration

**Environment Variables**:
- `ANALYTICS_BACKEND` - Storage backend (`jsonl`, `sqlite`, `loki`)
- `ANALYTICS_DIR` - Storage directory (default: `./ollama_analytics`)
- `ANALYTICS_RETENTION_DAYS` - Data retention period (default: 7 days)
- `OLLAMA_HOST` - Internal: Controls Ollama server binding

**Port Configuration**:
- **11434**: Proxy port (what your apps connect to)
- **11435**: Internal Ollama port (hidden from users)
- Ports are automatically checked for conflicts on startup

## Prometheus Integration

Example `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'ollama'
    static_configs:
      - targets: ['localhost:11434']
```

Example queries:

```promql
# P95 latency by prompt category
histogram_quantile(0.95,
  rate(ollama_request_duration_seconds_bucket[5m])
) by (prompt_category)

# Token generation rate by model
rate(ollama_tokens_generated_sum[5m]) by (model)

# Average tokens per second
rate(ollama_tokens_per_second_sum[5m]) / rate(ollama_tokens_per_second_count[5m])
```

## Requirements

- **Platform**: Windows (primary), Linux/Mac (compatible)
- **Python**: 3.7+ with pip
- **Ollama**: Installed and accessible via command line
- **Dependencies**: `aiohttp`, `prometheus-client` (auto-installed)
- **Windows Service**: `pywin32` (auto-installed by service installer)

## Architecture

The proxy uses:
- Histograms for efficient Prometheus metrics
- Automatic prompt categorization to limit cardinality
- Async write queue for analytics storage
- Transparent request forwarding
- Windows service mode with system tray icon (🦙🔒)

## Troubleshooting

### Common Issues

**Port Conflicts**:
```bash
# Check what's using Ollama's default port
netstat -an | findstr :11434
# Stop existing Ollama instances
taskkill /f /im ollama.exe
```

**Python Issues**:
```bash
# Verify Python installation
python --version
# Install dependencies
pip install aiohttp prometheus-client
```

**Windows Service Issues**:
```bash
# Check service status
.\service_manager.ps1 status
# View service logs
type %TEMP%\ollama_service\ollama_service.log
# Test without installing
python ollama_service.py
```

**Connectivity Testing**:
```bash
# Test proxy health
curl http://localhost:11434/test
# Check metrics endpoint
curl http://localhost:11434/metrics
# Verify analytics
curl http://localhost:11434/analytics/stats
```

**Debug Mode**:
Check console output for detailed logging including:
- Port binding status
- Ollama connection health
- Request routing information
- Analytics write queue status
- Service installation logs in `%TEMP%\ollama_service\`

## License

MIT