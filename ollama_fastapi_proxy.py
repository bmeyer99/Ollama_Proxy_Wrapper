#!/usr/bin/env python3
"""
FastAPI Ollama Proxy - Python 3.13 Compatible
- All existing metrics and analytics preserved
- Uses FastAPI + httpx instead of aiohttp
- Same functionality, better compatibility
"""

import asyncio
import httpx
import json
import time
import re
import os
from fastapi import FastAPI, Request, Response
from fastapi.responses import StreamingResponse, PlainTextResponse
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
from starlette.background import BackgroundTask

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
    'Tokens generated per second distribution',
    ['model', 'prompt_category'],
    buckets=(1, 5, 10, 20, 50, 100, 200, 500)
)

request_counter = Counter(
    'ollama_requests_total',
    'Total number of requests',
    ['model', 'endpoint', 'prompt_category', 'status']
)

active_requests_gauge = Gauge(
    'ollama_active_requests',
    'Number of requests currently being processed'
)

# Analytics storage (high detail, all data)
analytics_counter = Counter(
    'ollama_analytics_records_total',
    'Total analytics records written',
    ['backend']
)

class PromptCategorizer:
    """Categorizes prompts to maintain low cardinality for Prometheus"""
    
    def __init__(self):
        self.categories = {}
        self.patterns = [
            (r'\b(summarize|summary|tldr)\b', 'summarize'),
            (r'\b(translate|translation)\b', 'translate'), 
            (r'\b(explain|description|describe)\b', 'explain'),
            (r'\b(code|programming|script|function)\b', 'code'),
            (r'\b(question|answer|qa)\b', 'qa'),
            (r'\b(write|create|generate)\b', 'generate'),
            (r'\b(fix|debug|error)\b', 'debug'),
            (r'\b(analyze|analysis)\b', 'analyze'),
            (r'\b(compare|comparison|vs)\b', 'compare'),
            (r'\b(help|assist|support)\b', 'help'),
        ]
        
    def categorize(self, prompt):
        """Categorize a prompt, limiting total categories for cardinality"""
        if not prompt:
            return 'empty'
            
        prompt_lower = prompt.lower()
        
        # Check existing patterns
        for pattern, category in self.patterns:
            if re.search(pattern, prompt_lower):
                return category
        
        # Hash-based bucketing for unknown prompts
        if len(self.categories) < MAX_PROMPT_CATEGORIES:
            prompt_hash = hashlib.md5(prompt_lower.encode()).hexdigest()[:8]
            category = f'other_{prompt_hash}'
            self.categories[prompt_hash] = category
            return category
        
        return 'other_overflow'

class AnalyticsWriter:
    """Handles high-detail analytics writing with various backends"""
    
    def __init__(self, backend=ANALYTICS_BACKEND):
        self.backend = backend
        self.base_dir = Path(ANALYTICS_DIR)
        self.base_dir.mkdir(exist_ok=True)
        self.write_queue = Queue()
        self.running = True
        
        # Initialize backend
        if backend == 'sqlite':
            self._init_sqlite()
        elif backend == 'loki':
            self._init_loki()
        
        # Start background writer thread
        self.writer_thread = threading.Thread(target=self._writer_loop, daemon=True)
        self.writer_thread.start()
    
    def _init_sqlite(self):
        """Initialize SQLite database"""
        self.db_path = self.base_dir / 'ollama_analytics.db'
        conn = sqlite3.connect(self.db_path)
        conn.execute('''
            CREATE TABLE IF NOT EXISTS requests (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                timestamp TEXT,
                model TEXT,
                endpoint TEXT,
                prompt TEXT,
                prompt_category TEXT,
                response_preview TEXT,
                duration_seconds REAL,
                tokens_generated INTEGER,
                tokens_per_second REAL,
                status_code INTEGER,
                error_message TEXT,
                user_agent TEXT,
                client_ip TEXT
            )
        ''')
        conn.execute('''
            CREATE INDEX IF NOT EXISTS idx_timestamp ON requests(timestamp);
        ''')
        conn.execute('''
            CREATE INDEX IF NOT EXISTS idx_model ON requests(model);
        ''')
        conn.execute('''
            CREATE INDEX IF NOT EXISTS idx_prompt_category ON requests(prompt_category);
        ''')
        conn.commit()
        conn.close()
    
    def _init_loki(self):
        """Initialize Loki client"""
        self.loki_url = os.getenv('LOKI_URL', 'http://localhost:3100')
        logger.info(f"Loki backend configured for {self.loki_url}")
    
    def record(self, record):
        """Queue a record for writing"""
        try:
            self.write_queue.put(record, timeout=1)
        except:
            logger.warning("Analytics queue full, dropping record")
    
    def _writer_loop(self):
        """Background thread for writing analytics"""
        while self.running:
            try:
                record = self.write_queue.get(timeout=5)
                self._write_record(record)
                analytics_counter.labels(backend=self.backend).inc()
            except:
                continue
    
    def _write_record(self, record):
        """Write a single record to the backend"""
        try:
            if self.backend == 'jsonl':
                self._write_jsonl(record)
            elif self.backend == 'sqlite':
                self._write_sqlite(record)
            elif self.backend == 'loki':
                self._write_loki(record)
        except Exception as e:
            logger.error(f"Failed to write analytics record: {e}")
    
    def _write_jsonl(self, record):
        """Write to compressed JSONL file"""
        date_str = datetime.now().strftime('%Y-%m-%d')
        file_path = self.base_dir / f'ollama_analytics_{date_str}.jsonl.gz'
        
        with gzip.open(file_path, 'at', encoding='utf-8') as f:
            json.dump(record, f, default=str)
            f.write('\n')
    
    def _write_sqlite(self, record):
        """Write to SQLite database"""
        conn = sqlite3.connect(self.db_path)
        conn.execute('''
            INSERT INTO requests (
                timestamp, model, endpoint, prompt, prompt_category,
                response_preview, duration_seconds, tokens_generated,
                tokens_per_second, status_code, error_message,
                user_agent, client_ip
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ''', (
            record['timestamp'], record['model'], record['endpoint'],
            record['prompt'], record['prompt_category'], record['response_preview'],
            record['duration_seconds'], record['tokens_generated'],
            record['tokens_per_second'], record['status_code'],
            record.get('error_message'), record.get('user_agent'),
            record.get('client_ip')
        ))
        conn.commit()
        conn.close()
    
    def _write_loki(self, record):
        """Write to Loki (placeholder for now)"""
        logger.debug(f"Would write to Loki: {record}")
    
    def stop(self):
        """Stop the analytics writer"""
        self.running = False
        if hasattr(self, 'writer_thread'):
            self.writer_thread.join(timeout=5)

class FastAPIOllamaProxy:
    """FastAPI-based Ollama proxy with metrics and analytics"""
    
    def __init__(self, ollama_host='http://localhost:11435', proxy_port=11434, analytics_backend='sqlite'):
        self.ollama_host = ollama_host
        self.proxy_port = proxy_port
        self.app = FastAPI(title="Ollama Metrics Proxy")
        self.categorizer = PromptCategorizer()
        self.analytics = AnalyticsWriter(analytics_backend)
        self.client = httpx.AsyncClient(timeout=300.0)  # 5 minute timeout
        
        # Setup routes
        self._setup_routes()
        
        # Recent requests for analytics dashboard (keep last 100)
        self.recent_requests = deque(maxlen=100)
        
    def _setup_routes(self):
        """Setup FastAPI routes"""
        
        # Metrics endpoint
        @self.app.get("/metrics")
        async def metrics():
            return PlainTextResponse(
                generate_latest(),
                media_type=CONTENT_TYPE_LATEST
            )
        
        # Analytics endpoints
        @self.app.get("/analytics/stats")
        async def analytics_stats():
            return await self._analytics_stats()
        
        @self.app.get("/analytics/search")
        async def analytics_search(
            model: str = None,
            prompt_search: str = None,
            start_time: int = None,
            end_time: int = None,
            limit: int = 100
        ):
            return await self._analytics_search(model, prompt_search, start_time, end_time, limit)
        
        @self.app.get("/analytics")
        async def analytics_dashboard():
            return await self._analytics_dashboard()
        
        # Test endpoint
        @self.app.get("/test")
        async def test():
            return {"status": "ok", "proxy": "FastAPI Ollama Proxy", "backend": self.ollama_host}
        
        # Main proxy route - catch all
        @self.app.api_route("/{path:path}", methods=["GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"])
        async def proxy_request(request: Request, path: str):
            return await self._proxy_request(request, path)
    
    async def _proxy_request(self, request: Request, path: str):
        """Main proxy request handler"""
        start_time = time.time()
        active_requests_gauge.inc()
        
        try:
            # Build target URL
            target_url = f"{self.ollama_host}/{path}"
            if request.url.query:
                target_url += f"?{request.url.query}"
            
            # Get request body
            body = await request.body()
            
            # Parse request for metrics
            model, prompt, endpoint = await self._parse_request(request, body, path)
            prompt_category = self.categorizer.categorize(prompt)
            
            # Check if this is a streaming endpoint
            is_streaming = endpoint in ['generate', 'chat'] and request.method == 'POST'
            
            logger.info(f"Request to {endpoint}, streaming: {is_streaming}, method: {request.method}")
            
            if is_streaming:
                # Handle streaming response
                async def stream_generator():
                    response_text = ""
                    tokens = 0
                    try:
                        # Create proper headers for forwarding
                        headers = dict(request.headers)
                        headers.pop('host', None)  # Remove host header
                        headers.pop('content-length', None)  # Let httpx calculate
                        
                        logger.info(f"Streaming request to {target_url}")
                        
                        async with self.client.stream(
                            method=request.method,
                            url=target_url,
                            headers=headers,
                            content=body,
                            timeout=httpx.Timeout(300.0, connect=30.0)
                        ) as response:
                            logger.info(f"Got streaming response: {response.status_code}")
                            
                            async for chunk in response.aiter_bytes():
                                if chunk:
                                    yield chunk
                                    # Try to parse chunk for metrics
                                    try:
                                        response_text += chunk.decode('utf-8', errors='ignore')
                                        # Count tokens in streaming response
                                        if '"response":"' in response_text:
                                            tokens = len(response_text.split())
                                    except:
                                        pass
                        
                        # Record metrics after streaming completes
                        duration = time.time() - start_time
                        tokens_per_second = tokens / duration if duration > 0 and tokens > 0 else 0
                        await self._record_interaction(
                            model, endpoint, prompt, prompt_category, response_text[:200],
                            duration, tokens, tokens_per_second,
                            200, None, request
                        )
                    except Exception as e:
                        logger.error(f"Streaming error: {e}")
                        yield json.dumps({"error": str(e)}).encode()
                
                return StreamingResponse(
                    stream_generator(),
                    media_type="application/x-ndjson"
                )
            else:
                # Non-streaming response
                response = await self.client.request(
                    method=request.method,
                    url=target_url,
                    headers=dict(request.headers),
                    content=body,
                    timeout=300.0
                )
                
                # Parse response for metrics
                tokens_generated, response_preview = await self._parse_response(response, endpoint)
                
                # Calculate metrics
                duration = time.time() - start_time
                tokens_per_second = tokens_generated / duration if duration > 0 and tokens_generated > 0 else 0
                
                # Record metrics and analytics
                await self._record_interaction(
                    model, endpoint, prompt, prompt_category, response_preview,
                    duration, tokens_generated, tokens_per_second,
                    response.status_code, None, request
                )
                
                # Return response
                return Response(
                    content=response.content,
                    status_code=response.status_code,
                    headers=dict(response.headers),
                    background=BackgroundTask(self._cleanup_response, response)
                )
            
        except Exception as e:
            duration = time.time() - start_time
            logger.error(f"Proxy error: {e}")
            
            # Record error
            await self._record_interaction(
                model or 'unknown', endpoint or path, prompt or '', 
                prompt_category or 'error', '', duration, 0, 0,
                500, str(e), request
            )
            
            return Response(
                content=json.dumps({"error": "Proxy error", "detail": str(e)}),
                status_code=500,
                media_type="application/json"
            )
        
        finally:
            active_requests_gauge.dec()
    
    async def _cleanup_response(self, response):
        """Cleanup response resources"""
        try:
            await response.aclose()
        except:
            pass
    
    async def _parse_request(self, request: Request, body: bytes, path: str):
        """Parse request to extract model, prompt, and endpoint"""
        try:
            if body:
                data = json.loads(body.decode('utf-8'))
                model = data.get('model', 'unknown')
                prompt = data.get('prompt', '') or data.get('messages', [{}])[-1].get('content', '') if data.get('messages') else ''
            else:
                model = 'unknown'
                prompt = ''
            
            # Extract endpoint from path (e.g., "api/generate" -> "generate")
            if path.startswith('api/'):
                endpoint = path.replace('api/', '', 1)
            else:
                endpoint = path.split('/')[-1] if '/' in path else path
            return model, prompt, endpoint
            
        except:
            return 'unknown', '', path
    
    async def _parse_response(self, response, endpoint):
        """Parse response to extract tokens and preview"""
        try:
            if endpoint in ['generate', 'chat']:
                response_text = response.text
                if response_text:
                    data = json.loads(response_text)
                    tokens_generated = len(data.get('response', '').split()) if data.get('response') else 0
                    response_preview = data.get('response', '')[:200] if data.get('response') else ''
                    return tokens_generated, response_preview
        except:
            pass
        
        return 0, response.text[:200] if hasattr(response, 'text') else ''
    
    async def _record_interaction(self, model, endpoint, prompt, prompt_category, 
                                 response_preview, duration, tokens_generated, 
                                 tokens_per_second, status_code, error_message, request):
        """Record interaction in both metrics and analytics"""
        
        # Prometheus metrics (low cardinality)
        status = 'success' if status_code < 400 else 'error'
        
        request_duration_histogram.labels(
            model=model, endpoint=endpoint, prompt_category=prompt_category
        ).observe(duration)
        
        request_counter.labels(
            model=model, endpoint=endpoint, prompt_category=prompt_category, status=status
        ).inc()
        
        if tokens_generated > 0:
            tokens_generated_histogram.labels(
                model=model, prompt_category=prompt_category
            ).observe(tokens_generated)
            
            if tokens_per_second > 0:
                tokens_per_second_histogram.labels(
                    model=model, prompt_category=prompt_category
                ).observe(tokens_per_second)
        
        # High-detail analytics
        record = {
            'timestamp': datetime.now().isoformat(),
            'model': model,
            'endpoint': endpoint,
            'prompt': prompt[:1000],  # Truncate very long prompts
            'prompt_category': prompt_category,
            'response_preview': response_preview,
            'duration_seconds': duration,
            'tokens_generated': tokens_generated,
            'tokens_per_second': tokens_per_second,
            'status_code': status_code,
            'error_message': error_message,
            'user_agent': request.headers.get('user-agent', ''),
            'client_ip': request.client.host if request.client else ''
        }
        
        self.analytics.record(record)
        self.recent_requests.append(record)
        
        # Log summary
        logger.info(f"{model}/{prompt_category} - {duration:.2f}s - "
                   f"{tokens_generated} tokens - {status_code}")
    
    async def _analytics_stats(self):
        """Get analytics statistics"""
        stats = {
            'proxy_uptime_seconds': time.time() - getattr(self, 'start_time', time.time()),
            'total_requests': len(self.recent_requests),
            'recent_requests': list(self.recent_requests)[-10:],  # Last 10
            'backend': self.analytics.backend
        }
        return stats
    
    async def _analytics_search(self, model, prompt_search, start_time, end_time, limit):
        """Search analytics (SQLite only for now)"""
        if self.analytics.backend != 'sqlite':
            return {"error": "Search only available with SQLite backend"}
        
        try:
            conn = sqlite3.connect(self.analytics.db_path)
            query = "SELECT * FROM requests WHERE 1=1"
            params = []
            
            if model:
                query += " AND model = ?"
                params.append(model)
            
            if prompt_search:
                query += " AND prompt LIKE ?"
                params.append(f'%{prompt_search}%')
            
            if start_time:
                query += " AND timestamp >= ?"
                params.append(datetime.fromtimestamp(start_time).isoformat())
            
            if end_time:
                query += " AND timestamp <= ?"
                params.append(datetime.fromtimestamp(end_time).isoformat())
            
            query += " ORDER BY timestamp DESC LIMIT ?"
            params.append(limit)
            
            cursor = conn.execute(query, params)
            columns = [description[0] for description in cursor.description]
            results = [dict(zip(columns, row)) for row in cursor.fetchall()]
            conn.close()
            
            return {"results": results, "count": len(results)}
            
        except Exception as e:
            return {"error": f"Search failed: {e}"}
    
    async def _analytics_dashboard(self):
        """Simple HTML analytics dashboard"""
        html = """
        <!DOCTYPE html>
        <html>
        <head>
            <title>Ollama Analytics Dashboard</title>
            <style>
                body { font-family: Arial, sans-serif; margin: 20px; }
                .metric { background: #f0f0f0; padding: 10px; margin: 10px 0; border-radius: 5px; }
                .recent { max-height: 400px; overflow-y: auto; }
                table { border-collapse: collapse; width: 100%; }
                th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
                th { background-color: #f2f2f2; }
            </style>
        </head>
        <body>
            <h1>🦙 Ollama Metrics Proxy Dashboard</h1>
            <div class="metric"><strong>Proxy:</strong> FastAPI + httpx</div>
            <div class="metric"><strong>Backend:</strong> {backend}</div>
            <div class="metric"><strong>Total Requests:</strong> {total}</div>
            
            <h2>Recent Requests</h2>
            <div class="recent">
                <table>
                    <tr><th>Time</th><th>Model</th><th>Category</th><th>Duration</th><th>Tokens</th><th>Status</th></tr>
                    {recent_rows}
                </table>
            </div>
            
            <h2>API Endpoints</h2>
            <ul>
                <li><a href="/metrics">Prometheus Metrics</a></li>
                <li><a href="/analytics/stats">Statistics JSON</a></li>
                <li><a href="/analytics/search">Search API</a></li>
                <li><a href="/test">Test Endpoint</a></li>
            </ul>
        </body>
        </html>
        """
        
        recent_rows = ""
        for req in list(self.recent_requests)[-20:]:
            recent_rows += f"""
            <tr>
                <td>{req['timestamp'][:19]}</td>
                <td>{req['model']}</td>
                <td>{req['prompt_category']}</td>
                <td>{req['duration_seconds']:.2f}s</td>
                <td>{req['tokens_generated']}</td>
                <td>{req['status_code']}</td>
            </tr>
            """
        
        return Response(
            content=html.format(
                backend=self.analytics.backend,
                total=len(self.recent_requests),
                recent_rows=recent_rows
            ),
            media_type="text/html"
        )
    
    def run(self):
        """Run the proxy server"""
        self.start_time = time.time()
        logger.info(f"Starting FastAPI Ollama Proxy on port {self.proxy_port}")
        logger.info(f"Proxying to Ollama at {self.ollama_host}")
        logger.info(f"Analytics backend: {self.analytics.backend}")
        logger.info(f"Metrics: http://localhost:{self.proxy_port}/metrics")
        logger.info(f"Analytics Dashboard: http://localhost:{self.proxy_port}/analytics")
        if self.analytics.backend != 'sqlite':
            logger.warning("Analytics dashboard requires sqlite backend for full functionality")
        
        import uvicorn
        uvicorn.run(self.app, host='0.0.0.0', port=self.proxy_port)

if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser(description='FastAPI Ollama Proxy')
    parser.add_argument('--ollama-host', default='http://localhost:11435')
    parser.add_argument('--proxy-port', type=int, default=11434)
    parser.add_argument('--analytics-backend', choices=['jsonl', 'sqlite', 'loki'], 
                       default=ANALYTICS_BACKEND)
    args = parser.parse_args()
    
    proxy = FastAPIOllamaProxy(args.ollama_host, args.proxy_port, args.analytics_backend)
    proxy.run()