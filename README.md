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
- `http://localhost:11434/analytics` - Interactive analytics dashboard
- `http://localhost:11434/analytics/search` - Query analytics API

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
- **`analytics_dashboard.html`** - Interactive web dashboard for exploring analytics
- **`Grafana/Provisioning/Dashboards/`** - Pre-configured Grafana dashboard
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

The analytics dashboard uses these APIs - you can also query them directly:

```bash
# Interactive dashboard (recommended)
open http://localhost:11434/analytics

# Get analytics statistics
curl http://localhost:11434/analytics/stats

# Get all messages with filters
curl "http://localhost:11434/analytics/messages?model=phi4&limit=50"

# Get specific message details
curl "http://localhost:11434/analytics/messages/abc123"

# Get list of models
curl http://localhost:11434/analytics/models

# Search with multiple filters
curl "http://localhost:11434/analytics/messages?search=summarize&start_time=1640995200&status=success"

# Export as CSV
curl "http://localhost:11434/analytics/export?format=csv&model=phi4" -o analytics.csv
```

**Available Search Parameters**:
- `search` - Full-text search in prompts
- `start_time`, `end_time` - Unix timestamps
- `model` - Filter by model name
- `status` - Filter by status (success, error, timeout)
- `min_input_tokens`, `max_input_tokens` - Token count range
- `min_latency`, `max_latency` - Response time range (ms)
- `limit`, `offset` - Pagination
- `format` - Export format (json, csv)

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

## Monitoring Dashboards

### 1. Interactive Analytics Dashboard

Access at `http://localhost:11434/analytics` (requires SQLite backend)

**Features**:
- **Search & Filter**: Full-text search across prompts with advanced filtering
- **Interactive Charts**: Click on data points to drill down
  - Message timeline with zoom and time selection
  - Prompt category distribution
  - Token usage scatter plot
  - Response time histogram
- **Message Explorer**: Browse all requests with click-through to details
- **Detailed View**: See full prompts, responses, performance metrics
- **Export**: Download filtered data as CSV or JSON
- **Real-time Updates**: Auto-refresh every 30 seconds

**Enable Analytics Dashboard**:
```bash
# Start with SQLite backend for full analytics
set ANALYTICS_BACKEND=sqlite
python ollama_wrapper.py serve

# Visit http://localhost:11434/analytics
```

### 2. Grafana Dashboard

Pre-configured dashboard at `Grafana/Provisioning/Dashboards/grafana_ollama_dashboard.json`

**Panels Include**:
- Request rate and average latency summary cards
- Error rate and token generation speed
- Request rate & P95 latency by model (time series)
- Token generation speed comparison
- Request latency heatmap
- Time to first token by model
- Token usage breakdown (pie chart)
- Model performance comparison table
- Error tracking and active alerts

**Setup Grafana**:
```yaml
# docker-compose.yml
services:
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    volumes:
      - ./Grafana/Provisioning:/etc/grafana/provisioning
```

## Prometheus Integration

Example `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'ollama'
    static_configs:
      - targets: ['localhost:11434']
    scrape_interval: 10s
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