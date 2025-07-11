# Dashboard Implementation Guide

## Overview

This guide explains how to implement the two monitoring dashboards for your Ollama proxy:
1. **Grafana Overview Dashboard** - Real-time operational metrics
2. **Analytics Dashboard** - Deep-dive message analysis with drill-down

## Prerequisites

- Ollama proxy with Prometheus metrics (already implemented)
- Ollama proxy with analytics storage (SQLite backend recommended)
- Grafana instance for metrics dashboard
- Web server for analytics dashboard

## 1. Grafana Overview Dashboard Setup

### Step 1: Configure Prometheus

Ensure your Prometheus is scraping the Ollama proxy metrics:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'ollama'
    static_configs:
      - targets: ['localhost:11434']
    scrape_interval: 10s
```

### Step 2: Import Dashboard

1. Open Grafana and go to Dashboards → Import
2. Upload `grafana_ollama_dashboard.json`
3. Select your Prometheus data source
4. Click Import

### Step 3: Configure Alerts (Optional)

The dashboard includes alert panels. To enable alerts:

```yaml
# Example alert rules for prometheus
groups:
  - name: ollama_alerts
    interval: 30s
    rules:
      - alert: HighErrorRate
        expr: |
          (sum(rate(ollama_requests_total{status="error"}[5m])) / 
           sum(rate(ollama_requests_total[5m]))) > 0.05
        for: 5m
        annotations:
          summary: "Ollama error rate above 5%"
          
      - alert: HighLatency
        expr: |
          histogram_quantile(0.95, 
            sum by (le) (rate(ollama_request_duration_seconds_bucket[5m]))
          ) > 5
        for: 10m
        annotations:
          summary: "Ollama P95 latency above 5 seconds"
```

## 2. Analytics Dashboard Implementation

### Step 1: Extend Proxy API

Add these endpoints to `ollama_hybrid_proxy.py`:

```python
# Add to OllamaHybridProxy class

async def handle_analytics_api(self, request):
    """Handle analytics API requests"""
    path = request.path_qs
    
    if path.startswith('/analytics/messages'):
        return await self.get_messages(request)
    elif path.startswith('/analytics/aggregations'):
        return await self.get_aggregations(request)
    elif path.startswith('/analytics/models'):
        return await self.get_models(request)
    elif path.startswith('/analytics/export'):
        return await self.export_data(request)
    else:
        return web.Response(status=404)

async def get_messages(self, request):
    """Get filtered messages from analytics storage"""
    params = request.rel_url.query
    
    # Parse filters
    filters = {
        'search': params.get('search'),
        'model': params.get('model'),
        'start_time': params.get('start_time', type=int),
        'end_time': params.get('end_time', type=int),
        'status': params.get('status'),
        'min_input_tokens': params.get('min_input_tokens', type=int),
        'max_input_tokens': params.get('max_input_tokens', type=int),
        'min_latency': params.get('min_latency', type=float),
        'max_latency': params.get('max_latency', type=float),
        'limit': params.get('limit', 1000, type=int),
        'offset': params.get('offset', 0, type=int)
    }
    
    # Query analytics backend
    messages = await self.analytics_writer.query_messages(filters)
    
    return web.json_response(messages)

async def get_aggregations(self, request):
    """Get aggregated statistics"""
    params = request.rel_url.query
    
    aggregations = {
        'total_messages': await self.analytics_writer.count_messages(),
        'unique_users': await self.analytics_writer.count_unique_users(),
        'avg_tokens': await self.analytics_writer.avg_tokens(),
        'total_cost': await self.analytics_writer.total_cost(),
        'messages_by_time': await self.analytics_writer.messages_by_time(
            interval=params.get('interval', '1h')
        ),
        'messages_by_category': await self.analytics_writer.messages_by_category()
    }
    
    return web.json_response(aggregations)

async def get_models(self, request):
    """Get list of models"""
    models = await self.analytics_writer.get_models()
    return web.json_response(models)

async def export_data(self, request):
    """Export filtered data"""
    params = request.rel_url.query
    format = params.get('format', 'json')
    
    # Get filtered messages
    messages = await self.analytics_writer.query_messages(params)
    
    if format == 'csv':
        csv_data = self.messages_to_csv(messages)
        return web.Response(
            body=csv_data,
            headers={
                'Content-Type': 'text/csv',
                'Content-Disposition': 'attachment; filename="ollama_analytics.csv"'
            }
        )
    else:
        return web.json_response(messages)
```

### Step 2: Extend Analytics Writer

Add query methods to support the dashboard:

```python
# Add to AnalyticsWriter class

async def query_messages(self, filters):
    """Query messages with filters"""
    if self.backend == 'sqlite':
        query = "SELECT * FROM analytics WHERE 1=1"
        params = []
        
        if filters.get('search'):
            query += " AND (prompt LIKE ? OR response LIKE ?)"
            search_term = f"%{filters['search']}%"
            params.extend([search_term, search_term])
            
        if filters.get('model'):
            query += " AND model = ?"
            params.append(filters['model'])
            
        if filters.get('start_time'):
            query += " AND timestamp >= ?"
            params.append(filters['start_time'])
            
        if filters.get('end_time'):
            query += " AND timestamp <= ?"
            params.append(filters['end_time'])
            
        if filters.get('min_input_tokens'):
            query += " AND input_tokens >= ?"
            params.append(filters['min_input_tokens'])
            
        query += " ORDER BY timestamp DESC"
        query += f" LIMIT {filters.get('limit', 1000)}"
        query += f" OFFSET {filters.get('offset', 0)}"
        
        conn = sqlite3.connect(self.db_path)
        conn.row_factory = sqlite3.Row
        cursor = conn.cursor()
        
        rows = cursor.execute(query, params).fetchall()
        messages = [dict(row) for row in rows]
        
        conn.close()
        return messages
        
    elif self.backend == 'jsonl':
        # Implement JSONL filtering
        messages = []
        with gzip.open(self.current_file, 'rt') as f:
            for line in f:
                msg = json.loads(line)
                if self._matches_filters(msg, filters):
                    messages.append(msg)
        return messages[-filters.get('limit', 1000):]

async def count_messages(self):
    """Count total messages"""
    if self.backend == 'sqlite':
        conn = sqlite3.connect(self.db_path)
        count = conn.execute("SELECT COUNT(*) FROM analytics").fetchone()[0]
        conn.close()
        return count
    return 0

async def count_unique_users(self):
    """Count unique users"""
    if self.backend == 'sqlite':
        conn = sqlite3.connect(self.db_path)
        count = conn.execute("SELECT COUNT(DISTINCT user) FROM analytics").fetchone()[0]
        conn.close()
        return count
    return 0

async def avg_tokens(self):
    """Calculate average tokens"""
    if self.backend == 'sqlite':
        conn = sqlite3.connect(self.db_path)
        result = conn.execute("""
            SELECT 
                AVG(input_tokens) as avg_input,
                AVG(output_tokens) as avg_output
            FROM analytics
        """).fetchone()
        conn.close()
        return {
            'input': result[0] or 0,
            'output': result[1] or 0
        }
    return {'input': 0, 'output': 0}
```

### Step 3: Serve Analytics Dashboard

Add static file serving to the proxy:

```python
# Add to proxy initialization
app.router.add_static('/dashboard', path='./analytics_dashboard.html')

# Or serve through a dedicated web server like nginx:
# nginx.conf
server {
    listen 8080;
    location / {
        root /path/to/analytics_dashboard.html;
        try_files $uri /analytics_dashboard.html;
    }
    
    location /analytics/ {
        proxy_pass http://localhost:11434/analytics/;
    }
}
```

### Step 4: Configure Dashboard

Update the analytics dashboard JavaScript to point to your proxy:

```javascript
// In analytics_dashboard.html, update the API endpoints
const API_BASE = 'http://localhost:11434'; // Your proxy URL

async function refreshData() {
    const response = await fetch(`${API_BASE}/analytics/messages?` + params);
    // ... rest of the code
}
```

## 3. Production Deployment

### Security Considerations

1. **Authentication**: Add authentication to both dashboards
2. **CORS**: Configure CORS headers for analytics API
3. **Rate Limiting**: Implement rate limiting for API endpoints
4. **Data Privacy**: Consider PII masking in analytics

### Performance Optimization

1. **Caching**: Cache aggregations for frequently accessed data
2. **Indexing**: Add database indexes for common query patterns
3. **Pagination**: Implement cursor-based pagination for large datasets
4. **Data Retention**: Implement automatic cleanup of old data

### Monitoring the Monitors

1. **Dashboard Health**: Monitor dashboard query performance
2. **Storage Growth**: Track analytics database size
3. **API Latency**: Monitor analytics API response times

## 4. Customization Options

### Adding Custom Metrics

```python
# Example: Add model-specific cost tracking
OLLAMA_MODEL_COSTS = Histogram(
    'ollama_model_costs',
    'Cost per request by model',
    ['model'],
    buckets=[0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0]
)
```

### Custom Visualizations

```javascript
// Example: Add a custom chart for model comparison
function createModelComparisonChart() {
    const ctx = document.getElementById('modelComparison').getContext('2d');
    new Chart(ctx, {
        type: 'radar',
        data: {
            labels: ['Speed', 'Cost', 'Quality', 'Reliability'],
            datasets: models.map(model => ({
                label: model,
                data: getModelScores(model)
            }))
        }
    });
}
```

## 5. Troubleshooting

### Common Issues

1. **No metrics showing**: Check Prometheus scraping and metric names
2. **Analytics not loading**: Verify API endpoints and CORS settings
3. **High memory usage**: Implement data pagination and cleanup
4. **Slow queries**: Add database indexes and optimize queries

### Debug Commands

```bash
# Check if metrics are being exposed
curl http://localhost:11434/metrics | grep ollama_

# Test analytics API
curl http://localhost:11434/analytics/messages?limit=10

# Check database size (SQLite)
sqlite3 ollama_analytics.db "SELECT COUNT(*) FROM analytics;"

# Monitor proxy logs
tail -f ollama_proxy.log | grep -E "(ERROR|WARNING)"
```

## Next Steps

1. Deploy Grafana dashboard to production
2. Host analytics dashboard on web server
3. Configure alerts and notifications
4. Set up automated reports
5. Integrate with existing monitoring stack

This implementation provides comprehensive monitoring capabilities while maintaining the transparent proxy architecture.