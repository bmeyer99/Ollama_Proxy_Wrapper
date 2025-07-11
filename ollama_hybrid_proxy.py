#!/usr/bin/env python3
"""
Hybrid Ollama Proxy - Best of Both Worlds
- Efficient histograms for Prometheus monitoring
- Detailed logs for analytics and debugging
"""

import asyncio
import aiohttp
import json
import time
import re
import os
from aiohttp import web
from prometheus_client import Counter, Histogram, Gauge, generate_latest, CONTENT_TYPE_LATEST
from collections import deque
from datetime import datetime
import logging
import hashlib
from pathlib import Path
import gzip
import threading
from queue import Queue
import sqlite3
import traceback

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Configuration
ANALYTICS_BACKEND = os.getenv('ANALYTICS_BACKEND', 'jsonl')  # jsonl, sqlite, or loki
ANALYTICS_DIR = os.getenv('ANALYTICS_DIR', './ollama_analytics')
ANALYTICS_RETENTION_DAYS = int(os.getenv('ANALYTICS_RETENTION_DAYS', '7'))
MAX_PROMPT_CATEGORIES = 50

# Efficient Prometheus metrics (low cardinality)
request_duration_histogram = Histogram(
    'ollama_request_duration_seconds',
    'Request duration distribution',
    ['model', 'endpoint', 'prompt_category'],
    buckets=(0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0)
)

tokens_generated_histogram = Histogram(
    'ollama_tokens_generated',
    'Distribution of tokens generated',
    ['model', 'prompt_category'],
    buckets=(10, 50, 100, 250, 500, 1000, 2000, 5000)
)

tokens_per_second_histogram = Histogram(
    'ollama_tokens_per_second',
    'Distribution of token generation speed',
    ['model', 'prompt_category'],
    buckets=(1, 5, 10, 20, 30, 50, 75, 100, 150, 200)
)

requests_total = Counter(
    'ollama_requests_total',
    'Total number of requests',
    ['model', 'endpoint', 'status', 'prompt_category']
)

active_requests = Gauge(
    'ollama_active_requests',
    'Currently active requests',
    ['model']
)

analytics_queue_size = Gauge(
    'ollama_analytics_queue_size',
    'Number of interactions waiting to be written'
)

analytics_writes_total = Counter(
    'ollama_analytics_writes_total',
    'Total analytics records written',
    ['backend', 'status']
)

class PromptCategorizer:
    """Categorize prompts for metrics"""
    
    def __init__(self, max_categories=MAX_PROMPT_CATEGORIES):
        self.categories = {}
        self.max_categories = max_categories
        self.patterns = [
            (r'(?i)summar', 'summarize'),
            (r'(?i)translat', 'translate'),
            (r'(?i)explain', 'explain'),
            (r'(?i)write.*code', 'code_write'),
            (r'(?i)debug|fix', 'code_debug'),
            (r'(?i)question|what|how|why|when', 'question'),
            (r'(?i)creat|generat', 'creative'),
            (r'(?i)analyz|analy', 'analyze'),
            (r'(?i)help', 'help'),
            (r'(?i)list|enumerate', 'list'),
        ]
    
    def categorize(self, prompt: str) -> str:
        if not prompt:
            return 'empty'
        
        prompt_lower = prompt.lower()
        for pattern, category in self.patterns:
            if re.search(pattern, prompt_lower):
                return category
        
        first_word = prompt.split()[0].lower() if prompt.split() else 'other'
        
        if len(self.categories) < self.max_categories:
            self.categories[first_word] = True
            return first_word
        else:
            return 'other'

class AnalyticsWriter:
    """Handles writing detailed analytics to various backends"""
    
    def __init__(self, backend='jsonl', data_dir='./ollama_analytics'):
        self.backend = backend
        self.data_dir = Path(data_dir)
        self.data_dir.mkdir(exist_ok=True)
        
        # Async write queue
        self.write_queue = Queue()
        self.writer_thread = threading.Thread(target=self._writer_loop, daemon=True)
        self.writer_thread.start()
        
        # Initialize backend
        if backend == 'sqlite':
            self._init_sqlite()
    
    def _init_sqlite(self):
        """Initialize SQLite database"""
        db_path = self.data_dir / 'ollama_analytics.db'
        self.conn = sqlite3.connect(str(db_path), check_same_thread=False)
        self.conn.execute('''
            CREATE TABLE IF NOT EXISTS interactions (
                interaction_id TEXT PRIMARY KEY,
                timestamp REAL,
                model TEXT,
                endpoint TEXT,
                prompt_category TEXT,
                prompt_full TEXT,
                prompt_tokens INTEGER,
                generated_tokens INTEGER,
                tokens_per_second REAL,
                duration_seconds REAL,
                eval_duration_seconds REAL,
                load_duration_seconds REAL,
                status TEXT,
                error TEXT,
                metadata TEXT
            )
        ''')
        # Create indexes separately (sqlite3 can only execute one statement at a time)
        self.conn.execute('CREATE INDEX IF NOT EXISTS idx_timestamp ON interactions(timestamp)')
        self.conn.execute('CREATE INDEX IF NOT EXISTS idx_model ON interactions(model)')
        self.conn.execute('CREATE INDEX IF NOT EXISTS idx_category ON interactions(prompt_category)')
        self.conn.commit()
    
    def _writer_loop(self):
        """Background thread for writing analytics"""
        while True:
            try:
                record = self.write_queue.get()
                if record is None:  # Shutdown signal
                    break
                
                self._write_record(record)
                analytics_queue_size.set(self.write_queue.qsize())
                
            except Exception as e:
                logger.error(f"Analytics write error: {e}")
                analytics_writes_total.labels(backend=self.backend, status='error').inc()
    
    def _write_record(self, record):
        """Write a single record to the backend"""
        try:
            if self.backend == 'jsonl':
                self._write_jsonl(record)
            elif self.backend == 'sqlite':
                self._write_sqlite(record)
            elif self.backend == 'loki':
                self._write_loki(record)
            
            analytics_writes_total.labels(backend=self.backend, status='success').inc()
            
        except Exception as e:
            logger.error(f"Failed to write analytics record: {e}")
            analytics_writes_total.labels(backend=self.backend, status='error').inc()
    
    def _write_jsonl(self, record):
        """Write to compressed JSONL file"""
        date_str = datetime.fromtimestamp(record['timestamp']).strftime('%Y%m%d')
        filename = self.data_dir / f'ollama_{date_str}.jsonl.gz'
        
        with gzip.open(filename, 'at', encoding='utf-8') as f:
            f.write(json.dumps(record) + '\n')
    
    def _write_sqlite(self, record):
        """Write to SQLite database"""
        self.conn.execute('''
            INSERT OR REPLACE INTO interactions VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ''', (
            record['interaction_id'],
            record['timestamp'],
            record['model'],
            record['endpoint'],
            record['prompt_category'],
            record.get('prompt_full', ''),
            record.get('prompt_tokens', 0),
            record.get('generated_tokens', 0),
            record.get('tokens_per_second'),
            record['duration'],
            record.get('eval_duration'),
            record.get('load_duration'),
            record['status'],
            record.get('error'),
            json.dumps(record.get('metadata', {}))
        ))
        self.conn.commit()
    
    def _write_loki(self, record):
        """Write to Loki (log aggregation)"""
        # This would send to Loki API
        # For now, just log it
        logger.info(f"Would send to Loki: {record['interaction_id']}")
    
    def write(self, record):
        """Queue a record for writing"""
        self.write_queue.put(record)
        analytics_queue_size.set(self.write_queue.qsize())
    
    def search(self, query_params):
        """Search analytics (only for sqlite backend)"""
        if self.backend != 'sqlite':
            return {"error": "Search only available with sqlite backend"}
        
        conditions = []
        params = []
        
        # Time filters
        if 'start_time' in query_params:
            conditions.append('timestamp >= ?')
            params.append(float(query_params['start_time']))
        
        if 'end_time' in query_params:
            conditions.append('timestamp <= ?')
            params.append(float(query_params['end_time']))
        
        # Model filter
        if 'model' in query_params and query_params['model']:
            conditions.append('model = ?')
            params.append(query_params['model'])
        
        # Search filter (searches both prompt and response)
        if 'search' in query_params and query_params['search']:
            conditions.append('(prompt_full LIKE ? OR prompt_full LIKE ?)')
            search_term = f"%{query_params['search']}%"
            params.extend([search_term, search_term])
        
        # Legacy prompt_search support
        if 'prompt_search' in query_params and query_params['prompt_search']:
            conditions.append('prompt_full LIKE ?')
            params.append(f"%{query_params['prompt_search']}%")
        
        # Status filter
        if 'status' in query_params and query_params['status']:
            conditions.append('status = ?')
            params.append(query_params['status'])
        
        # Token filters
        if 'min_input_tokens' in query_params:
            conditions.append('prompt_tokens >= ?')
            params.append(int(query_params['min_input_tokens']))
        
        if 'max_input_tokens' in query_params:
            conditions.append('prompt_tokens <= ?')
            params.append(int(query_params['max_input_tokens']))
        
        # Latency filters (convert ms to seconds)
        if 'min_latency' in query_params:
            conditions.append('duration_seconds >= ?')
            params.append(float(query_params['min_latency']) / 1000)
        
        if 'max_latency' in query_params:
            conditions.append('duration_seconds <= ?')
            params.append(float(query_params['max_latency']) / 1000)
        
        where_clause = 'WHERE ' + ' AND '.join(conditions) if conditions else ''
        
        # Handle limit and offset
        limit = int(query_params.get('limit', 1000))
        offset = int(query_params.get('offset', 0))
        
        query = f'''
            SELECT 
                interaction_id as id,
                timestamp,
                model,
                endpoint,
                prompt_category as category,
                prompt_full as prompt,
                prompt_tokens as input_tokens,
                generated_tokens as output_tokens,
                tokens_per_second,
                duration_seconds as latency,
                eval_duration_seconds,
                load_duration_seconds,
                status,
                error,
                metadata,
                CASE 
                    WHEN prompt_tokens > 0 AND generated_tokens > 0 
                    THEN (prompt_tokens * 0.00001 + generated_tokens * 0.00003)
                    ELSE 0 
                END as cost
            FROM interactions
            {where_clause}
            ORDER BY timestamp DESC
            LIMIT {limit} OFFSET {offset}
        '''
        
        cursor = self.conn.execute(query, params)
        columns = [desc[0] for desc in cursor.description]
        results = []
        
        for row in cursor.fetchall():
            record = dict(zip(columns, row))
            if record['metadata']:
                try:
                    record['metadata'] = json.loads(record['metadata'])
                except:
                    record['metadata'] = {}
            
            # Add user from metadata if available
            if isinstance(record.get('metadata'), dict):
                record['user'] = record['metadata'].get('user', 'anonymous')
            else:
                record['user'] = 'anonymous'
                
            results.append(record)
        
        return results
    
    def cleanup_old_data(self):
        """Remove old analytics data"""
        cutoff_time = time.time() - (ANALYTICS_RETENTION_DAYS * 86400)
        
        if self.backend == 'sqlite':
            self.conn.execute('DELETE FROM interactions WHERE timestamp < ?', (cutoff_time,))
            self.conn.commit()
        elif self.backend == 'jsonl':
            # Remove old .jsonl.gz files
            cutoff_date = datetime.fromtimestamp(cutoff_time)
            for file in self.data_dir.glob('ollama_*.jsonl.gz'):
                file_date_str = file.stem.split('_')[1].split('.')[0]
                file_date = datetime.strptime(file_date_str, '%Y%m%d')
                if file_date < cutoff_date:
                    file.unlink()

class HybridOllamaProxy:
    def __init__(self, ollama_host: str = "http://localhost:11434", 
                 proxy_port: int = 11435, analytics_backend: str = 'jsonl'):
        self.ollama_host = ollama_host.rstrip('/')
        self.proxy_port = proxy_port
        self.app = web.Application()
        self.categorizer = PromptCategorizer()
        self.analytics = AnalyticsWriter(backend=analytics_backend, data_dir=ANALYTICS_DIR)
        self.setup_routes()
        
        # Periodic cleanup will be started when the event loop is running
        self.cleanup_task = None
    
    def setup_routes(self):
        # Monitoring endpoints
        self.app.router.add_get('/metrics', self.handle_metrics)
        
        # Analytics dashboard
        self.app.router.add_get('/analytics', self.handle_analytics_dashboard)
        self.app.router.add_get('/analytics/', self.handle_analytics_dashboard)
        
        # Analytics API endpoints
        self.app.router.add_get('/analytics/messages', self.handle_analytics_messages)
        self.app.router.add_get('/analytics/messages/{id}', self.handle_analytics_message_detail)
        self.app.router.add_get('/analytics/models', self.handle_analytics_models)
        self.app.router.add_get('/analytics/search', self.handle_analytics_search)
        self.app.router.add_get('/analytics/export', self.handle_analytics_export)
        self.app.router.add_get('/analytics/stats', self.handle_analytics_stats)
        
        # Test endpoint
        self.app.router.add_get('/test', self.handle_test)
        
        # Proxy all other requests
        self.app.router.add_route('*', '/{path:.*}', self.handle_proxy)
        
        # Startup event to create cleanup task
        async def on_startup(app):
            self.cleanup_task = asyncio.create_task(self._periodic_cleanup())
        
        self.app.on_startup.append(on_startup)
    
    async def _periodic_cleanup(self):
        """Cleanup old analytics data periodically"""
        while True:
            try:
                await asyncio.sleep(3600)  # Every hour
                self.analytics.cleanup_old_data()
                logger.info("Cleaned up old analytics data")
            except Exception as e:
                logger.error(f"Cleanup error: {e}")
    
    def generate_interaction_id(self) -> str:
        """Generate unique interaction ID"""
        return hashlib.sha256(f"{time.time()}:{os.urandom(8).hex()}".encode()).hexdigest()[:16]
    
    def extract_prompt_info(self, data: dict) -> tuple[str, str, str]:
        """Extract prompt details"""
        prompt_full = ""
        
        if isinstance(data, dict):
            if 'prompt' in data:
                prompt_full = str(data['prompt'])
            elif 'messages' in data and isinstance(data['messages'], list):
                for msg in data['messages']:
                    if isinstance(msg, dict) and msg.get('role') == 'user':
                        prompt_full = str(msg.get('content', ''))
                        break
        
        prompt_full = prompt_full.strip()
        prompt_preview = (prompt_full[:100] + "...") if len(prompt_full) > 100 else prompt_full
        prompt_category = self.categorizer.categorize(prompt_full)
        
        return prompt_full, prompt_preview, prompt_category
    
    async def handle_metrics(self, request):
        """Serve Prometheus metrics"""
        metrics = generate_latest()
        return web.Response(
            body=metrics,
            content_type='text/plain; version=0.0.4',
            charset='utf-8'
        )
    
    async def handle_analytics_dashboard(self, request):
        """Serve the analytics dashboard HTML"""
        dashboard_path = Path(__file__).parent / 'analytics_dashboard.html'
        if dashboard_path.exists():
            with open(dashboard_path, 'r') as f:
                html_content = f.read()
            # Update API endpoint to use relative URLs
            html_content = html_content.replace("'/analytics/", "'/analytics/")
            return web.Response(text=html_content, content_type='text/html')
        else:
            return web.Response(text="Analytics dashboard not found", status=404)
    
    async def handle_analytics_messages(self, request):
        """Get filtered messages for analytics dashboard"""
        if self.analytics.backend != 'sqlite':
            return web.json_response({
                "error": "Analytics only available with sqlite backend. Current: " + self.analytics.backend
            }, status=400)
        
        results = self.analytics.search(dict(request.query))
        return web.json_response(results)
    
    async def handle_analytics_message_detail(self, request):
        """Get detailed message by ID"""
        message_id = request.match_info['id']
        if self.analytics.backend != 'sqlite':
            return web.json_response({
                "error": "Analytics only available with sqlite backend"
            }, status=400)
        
        conn = self.analytics.conn
        cursor = conn.execute('''
            SELECT 
                interaction_id as id,
                timestamp,
                model,
                endpoint,
                prompt_category as category,
                prompt_full as prompt,
                prompt_tokens as input_tokens,
                generated_tokens as output_tokens,
                tokens_per_second,
                duration_seconds as latency,
                eval_duration_seconds as eval_duration,
                load_duration_seconds as load_duration,
                status,
                error,
                metadata,
                CASE 
                    WHEN prompt_tokens > 0 AND generated_tokens > 0 
                    THEN (prompt_tokens * 0.00001 + generated_tokens * 0.00003)
                    ELSE 0 
                END as cost
            FROM interactions 
            WHERE interaction_id = ?
        ''', (message_id,))
        
        columns = [desc[0] for desc in cursor.description]
        row = cursor.fetchone()
        
        if row:
            record = dict(zip(columns, row))
            if record['metadata']:
                try:
                    record['metadata'] = json.loads(record['metadata'])
                except:
                    record['metadata'] = {}
            
            # Add calculated fields
            record['user'] = record.get('metadata', {}).get('user', 'anonymous')
            record['response'] = 'Response data not stored in current version'
            record['queue_time'] = 0  # Would need to track this separately
            record['time_to_first_token'] = record.get('eval_duration', 0) * 0.1 if record.get('eval_duration') else 0
            
            return web.json_response(record)
        else:
            return web.json_response({"error": "Message not found"}, status=404)
    
    async def handle_analytics_models(self, request):
        """Get list of models from analytics"""
        if self.analytics.backend != 'sqlite':
            return web.json_response([])
        
        conn = self.analytics.conn
        cursor = conn.execute('SELECT DISTINCT model FROM interactions WHERE model IS NOT NULL')
        models = [row[0] for row in cursor.fetchall()]
        return web.json_response(models)
    
    async def handle_analytics_search(self, request):
        """Search analytics data"""
        if self.analytics.backend != 'sqlite':
            return web.json_response({
                "error": "Search only available with sqlite backend. Current: " + self.analytics.backend
            }, status=400)
        
        results = self.analytics.search(dict(request.query))
        return web.json_response(results)
    
    async def handle_analytics_export(self, request):
        """Export analytics data"""
        if self.analytics.backend != 'sqlite':
            return web.json_response({
                "error": "Export only available with sqlite backend"
            }, status=400)
        
        format_type = request.query.get('format', 'json')
        results = self.analytics.search(dict(request.query))
        
        if format_type == 'csv':
            import csv
            import io
            
            output = io.StringIO()
            if results:
                writer = csv.DictWriter(output, fieldnames=results[0].keys())
                writer.writeheader()
                writer.writerows(results)
            
            return web.Response(
                text=output.getvalue(),
                content_type='text/csv',
                headers={'Content-Disposition': 'attachment; filename="ollama_analytics.csv"'}
            )
        else:
            return web.json_response(results)
    
    async def handle_analytics_stats(self, request):
        """Get analytics statistics"""
        stats = {
            "backend": self.analytics.backend,
            "data_dir": str(self.analytics.data_dir),
            "queue_size": self.analytics.write_queue.qsize(),
            "retention_days": ANALYTICS_RETENTION_DAYS,
            "categories": list(self.categorizer.categories.keys())
        }
        return web.json_response(stats)
    
    async def handle_test(self, request):
        """Test connectivity to Ollama"""
        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(
                    f"{self.ollama_host}/api/tags",
                    timeout=aiohttp.ClientTimeout(total=5)
                ) as response:
                    data = await response.json()
                    return web.json_response({
                        "status": "ok",
                        "ollama_host": self.ollama_host,
                        "ollama_reachable": True,
                        "models": [m["name"] for m in data.get("models", [])]
                    })
        except Exception as e:
            return web.json_response({
                "status": "error",
                "ollama_host": self.ollama_host,
                "ollama_reachable": False,
                "error": str(e)
            }, status=500)
    
    async def handle_proxy(self, request):
        """Proxy request with hybrid metric/analytics collection"""
        path = request.match_info['path']
        if not path:
            path = ''
        
        target_url = f"{self.ollama_host}/{path}"
        
        logger.info(f"🔄 Proxying {request.method} /{path} to {target_url}")
        
        # Initialize interaction record
        interaction_id = self.generate_interaction_id()
        interaction_record = {
            'interaction_id': interaction_id,
            'timestamp': time.time(),
            'endpoint': path,
            'model': 'unknown',
            'prompt_category': 'unknown',
            'status': 'started',
            'metadata': {
                'client_ip': request.remote,
                'user_agent': request.headers.get('User-Agent', '')
            }
        }
        
        # Parse request
        body = None
        if request.body_exists:
            body = await request.read()
            logger.info(f"Request body: {body[:200]}")  # Log first 200 chars
            try:
                data = json.loads(body)
                interaction_record['model'] = data.get('model', 'unknown')
                prompt_full, prompt_preview, prompt_category = self.extract_prompt_info(data)
                interaction_record['prompt_full'] = prompt_full
                interaction_record['prompt_preview'] = prompt_preview
                interaction_record['prompt_category'] = prompt_category
            except Exception as e:
                logger.error(f"Failed to parse body: {e}")
                pass
        
        model = interaction_record['model']
        prompt_category = interaction_record['prompt_category']
        
        # Track active request
        active_requests.labels(model=model).inc()
        
        # Start timing
        start_time = time.time()
        
        try:
            async with aiohttp.ClientSession() as session:
                # Add timeout and better error handling
                logger.info(f"Forwarding {request.method} request to {target_url}")
                
                async with session.request(
                    method=request.method,
                    url=target_url,
                    headers={k: v for k, v in request.headers.items() 
                            if k.lower() not in ['host', 'content-length', 'transfer-encoding']},
                    data=body,
                    params=request.query,
                    allow_redirects=False,
                    timeout=aiohttp.ClientTimeout(total=30)  # 30 second timeout
                ) as response:
                    
                    logger.info(f"Got response: {response.status}")
                    
                    # Handle response
                    if 'stream' in request.headers.get('accept', '').lower() or \
                       (isinstance(body, bytes) and b'"stream":true' in body):
                        result = await self.handle_streaming_response(
                            response, interaction_record, model, path, prompt_category, start_time
                        )
                        return result
                    else:
                        content = await response.read()
                        
                        # Extract metrics
                        try:
                            response_data = json.loads(content)
                            metrics = self.extract_metrics(response_data)
                            interaction_record.update(metrics)
                        except:
                            pass
                        
                        # Record everything
                        self.record_interaction(interaction_record, model, path, 
                                              prompt_category, start_time, 'success')
                        
                        return web.Response(
                            body=content,
                            status=response.status,
                            headers={k: v for k, v in response.headers.items()
                                   if k.lower() not in ['content-encoding', 'content-length', 'transfer-encoding']}
                        )
                        
        except Exception as e:
            logger.error(f"Proxy error: {e}")
            logger.error(f"Full traceback: {traceback.format_exc()}")
            interaction_record['error'] = str(e)
            self.record_interaction(interaction_record, model, path, 
                                  prompt_category, start_time, 'error')
            return web.Response(text=f"Proxy error: {str(e)}", status=500)
        finally:
            active_requests.labels(model=model).dec()
    
    async def handle_streaming_response(self, response, interaction_record, 
                                      model, path, prompt_category, start_time):
        """Handle streaming with metric collection"""
        resp = web.StreamResponse()
        resp.headers['Content-Type'] = 'application/json'
        await resp.prepare(resp)
        
        metrics = {}
        
        async for line in response.content:
            await resp.write(line)
            
            try:
                line_str = line.decode('utf-8').strip()
                if line_str:
                    data = json.loads(line_str)
                    current_metrics = self.extract_metrics(data)
                    metrics.update(current_metrics)
            except:
                pass
        
        # Update interaction record with final metrics
        interaction_record.update(metrics)
        
        # Record everything
        self.record_interaction(interaction_record, model, path, 
                              prompt_category, start_time, 'success')
        
        await resp.write_eof()
        return resp
    
    def extract_metrics(self, data: dict) -> dict:
        """Extract metrics from response"""
        metrics = {}
        
        if 'eval_count' in data:
            metrics['generated_tokens'] = data['eval_count']
            if 'eval_duration' in data and data['eval_duration'] > 0:
                metrics['tokens_per_second'] = data['eval_count'] / (data['eval_duration'] / 1e9)
                metrics['eval_duration'] = data['eval_duration'] / 1e9
        
        if 'prompt_eval_count' in data:
            metrics['prompt_tokens'] = data['prompt_eval_count']
        
        if 'load_duration' in data:
            metrics['load_duration'] = data['load_duration'] / 1e9
        
        return metrics
    
    def record_interaction(self, interaction_record, model, endpoint, 
                          prompt_category, start_time, status):
        """Record metrics and analytics"""
        duration = time.time() - start_time
        interaction_record['duration'] = duration
        interaction_record['status'] = status
        
        # 1. Update Prometheus metrics (aggregated, low cardinality)
        request_duration_histogram.labels(
            model=model, endpoint=endpoint, prompt_category=prompt_category
        ).observe(duration)
        
        requests_total.labels(
            model=model, endpoint=endpoint, status=status, prompt_category=prompt_category
        ).inc()
        
        if 'generated_tokens' in interaction_record:
            tokens_generated_histogram.labels(
                model=model, prompt_category=prompt_category
            ).observe(interaction_record['generated_tokens'])
        
        if 'tokens_per_second' in interaction_record:
            tokens_per_second_histogram.labels(
                model=model, prompt_category=prompt_category
            ).observe(interaction_record['tokens_per_second'])
        
        # 2. Write detailed analytics (high cardinality, for analysis)
        self.analytics.write(interaction_record)
        
        logger.info(f"Recorded: {interaction_record['interaction_id']} - "
                   f"{model}/{prompt_category} - {duration:.2f}s")
    
    def run(self):
        self.start_time = time.time()
        logger.info(f"Starting Hybrid Ollama Proxy on port {self.proxy_port}")
        logger.info(f"Proxying to Ollama at {self.ollama_host}")
        logger.info(f"Analytics backend: {self.analytics.backend}")
        logger.info(f"Metrics: http://localhost:{self.proxy_port}/metrics")
        logger.info(f"Analytics Dashboard: http://localhost:{self.proxy_port}/analytics")
        if self.analytics.backend != 'sqlite':
            logger.warning("Analytics dashboard requires sqlite backend for full functionality")
        web.run_app(self.app, host='0.0.0.0', port=self.proxy_port)

if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser(description='Hybrid Ollama Proxy')
    parser.add_argument('--ollama-host', default='http://localhost:11434')
    parser.add_argument('--proxy-port', type=int, default=11435)
    parser.add_argument('--analytics-backend', choices=['jsonl', 'sqlite', 'loki'], 
                       default=ANALYTICS_BACKEND)
    args = parser.parse_args()
    
    proxy = HybridOllamaProxy(args.ollama_host, args.proxy_port, args.analytics_backend)
    proxy.run()