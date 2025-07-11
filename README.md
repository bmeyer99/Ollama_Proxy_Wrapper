# Ollama Transparent Metrics Proxy

A transparent proxy that adds Prometheus metrics and analytics to Ollama without requiring any changes to existing applications.

## What It Does

- Intercepts all Ollama API calls to collect metrics
- Exposes Prometheus metrics for monitoring (latency, tokens/sec, error rates)
- Stores detailed analytics for every request (prompts, responses, timings)
- Works transparently - your apps still connect to `localhost:11434`

## Quick Start

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

## Files

- `ollama_hybrid_proxy.py` - The metrics collection proxy
- `ollama_wrapper.py` - Manages Ollama and proxy startup
- `quick_install.bat` - Windows installer
- `test_metrics.ps1` - Verify installation

## Metrics Collected

### Prometheus Metrics (Low Cardinality)
- `ollama_requests_total` - Request count by model, endpoint, status
- `ollama_request_duration_seconds` - Latency histogram
- `ollama_tokens_per_second` - Generation speed histogram
- `ollama_tokens_total` - Total tokens processed

### Analytics Storage (High Detail)
- Full prompt text
- Response timings
- Token counts
- Error details
- Searchable SQLite database

## Advanced Features

### PowerShell Launcher

For additional features, use the PowerShell wrapper:

```powershell
.\ollama.ps1 run phi4        # Start with metrics
.\ollama.ps1 -Action status   # Check status
.\ollama.ps1 -Action test     # Test setup
```

### Analytics Backends

Configure storage backend via environment variable:

```bash
# Default: Compressed JSON files
set ANALYTICS_BACKEND=jsonl

# Option: SQLite database with search API
set ANALYTICS_BACKEND=sqlite
```

### Analytics Queries

With SQLite backend, query historical data:

```bash
# Find slow requests
curl "http://localhost:11434/analytics/search?min_duration=10"

# Search by prompt content
curl "http://localhost:11434/analytics/search?prompt_search=summarize"

# Filter by model
curl "http://localhost:11434/analytics/search?model=phi4"
```

### Configuration

Environment variables:
- `ANALYTICS_BACKEND` - Storage backend (jsonl, sqlite)
- `ANALYTICS_DIR` - Storage directory (default: ./ollama_analytics)
- `ANALYTICS_RETENTION_DAYS` - Data retention period (default: 7)

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

# Tokens per second by model
rate(ollama_tokens_total[5m]) by (model)
```

## Requirements

- Windows (Linux/Mac support planned)
- Python 3.7+
- Ollama installed

## Architecture

The proxy uses:
- Histograms for efficient Prometheus metrics
- Automatic prompt categorization to limit cardinality
- Async write queue for analytics storage
- Transparent request forwarding

## Troubleshooting

**Port already in use**: Stop existing Ollama instances first

**Python not found**: Install Python and ensure it's in PATH

**Missing modules**: Run `pip install aiohttp prometheus-client`

## License

MIT