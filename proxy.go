package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Proxy handles HTTP reverse proxy with metrics collection
type Proxy struct {
	target        *url.URL
	reverseProxy  *httputil.ReverseProxy
	port          int
	metrics       *MetricsCollector
	analytics     *AnalyticsWriter
	server        *http.Server
	maxConcurrent chan struct{} // Semaphore for rate limiting
}

// NewProxy creates a new proxy instance
func NewProxy(targetURL string, port int, isService bool) *Proxy {
	target, err := url.Parse(targetURL)
	if err != nil {
		log.Fatalf("Invalid target URL: %v", err)
	}

	// Determine analytics directory based on execution context
	var analyticsDir string

	if isService {
		// Service mode: use ProgramData
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = "C:\\ProgramData"
		}
		analyticsDir = filepath.Join(programData, "OllamaProxy", "analytics")
	} else {
		// Console mode: use directory relative to executable
		if exePath, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exePath)
			analyticsDir = filepath.Join(exeDir, "ollama_analytics")
		} else {
			analyticsDir = filepath.Join(".", "ollama_analytics")
		}
	}
	
	// Ensure directory exists
	if err := os.MkdirAll(analyticsDir, 0755); err != nil {
		log.Printf("Warning: Failed to create analytics directory %s: %v", analyticsDir, err)
	}
	
	p := &Proxy{
		target:        target,
		port:          port,
		metrics:       NewMetricsCollector(),
		analytics:     NewAnalyticsWriter("sqlite", analyticsDir),
		maxConcurrent: make(chan struct{}, 50), // Limit to 50 concurrent requests
	}

	// Create custom transport with proper timeouts for Ollama
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true, // Let Ollama handle compression
		ForceAttemptHTTP2:   false,
		// Add explicit timeouts for service reliability
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second, // Give Ollama time to start processing
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Create reverse proxy with custom director
	p.reverseProxy = &httputil.ReverseProxy{
		Transport: transport,
		FlushInterval: 10 * time.Millisecond, // Small flush interval for streaming (not -1 which can cause issues in service mode)
		BufferPool: nil, // Use default buffer pool
		Director: func(req *http.Request) {
			// Save original host before modification
			originalHost := req.Host
			if originalHost == "" {
				originalHost = req.Header.Get("Host")
			}
			
			// IMPORTANT: Modify the existing URL in place, don't create a new one
			req.URL.Scheme = p.target.Scheme
			req.URL.Host = p.target.Host
			req.Host = p.target.Host
			
			// Add X-Forwarded headers
			if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
				req.Header.Set("X-Forwarded-For", clientIP)
			}
			req.Header.Set("X-Forwarded-Host", originalHost)
			req.Header.Set("X-Forwarded-Proto", "http")
			
			// Log the final request being sent
			// Optional: Log the final request being sent
			log.Printf("Director: Forwarding to %s%s", req.URL.Host, req.URL.Path)
		},
		ModifyResponse: p.modifyResponse,
		ErrorHandler:   p.errorHandler,
	}

	return p
}

// Start begins the proxy server
func (p *Proxy) Start() error {
	mux := http.NewServeMux()

	// Metrics endpoint
	mux.HandleFunc("/metrics", p.handleMetrics)

	// Analytics endpoints
	mux.HandleFunc("/analytics/stats", p.handleAnalyticsStats)
	mux.HandleFunc("/analytics/stats/enhanced", p.handleAnalyticsStatsEnhanced)
	mux.HandleFunc("/analytics/search", p.handleAnalyticsSearch)
	mux.HandleFunc("/analytics/messages", p.handleAnalyticsMessages)
	mux.HandleFunc("/analytics/messages/", p.handleAnalyticsMessageDetail)
	mux.HandleFunc("/analytics/models", p.handleAnalyticsModels)
	mux.HandleFunc("/analytics/export", p.handleAnalyticsExport)
	mux.HandleFunc("/analytics", p.handleAnalyticsDashboard)
	mux.HandleFunc("/analytics/", p.handleAnalyticsDashboard)

	// Test endpoint
	mux.HandleFunc("/test", p.handleTest)

	// Proxy all other requests
	mux.HandleFunc("/", p.handleProxy)

	// Create HTTP server with proper timeouts for graceful shutdown
	p.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", p.port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 90 * time.Second,  // Long timeout for streaming responses
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("Starting Ollama Proxy on port %d", p.port)
	log.Printf("Proxying to Ollama at %s", p.target)
	log.Printf("Metrics: http://localhost:%d/metrics", p.port)
	log.Printf("Analytics Dashboard: http://localhost:%d/analytics", p.port)

	// Structured logging for startup
	slog.Info("Proxy starting",
		"port", p.port,
		"target", p.target.String(),
		"metrics_endpoint", fmt.Sprintf("http://localhost:%d/metrics", p.port),
		"analytics_endpoint", fmt.Sprintf("http://localhost:%d/analytics", p.port),
	)

	return p.server.ListenAndServe()
}

// Shutdown gracefully shuts down the proxy
func (p *Proxy) Shutdown() {
	log.Printf("Shutting down proxy...")
	slog.Info("Initiating proxy shutdown")

	// Gracefully shutdown HTTP server with timeout
	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := p.server.Shutdown(ctx); err != nil {
			log.Printf("Proxy shutdown error: %v", err)
			slog.Error("HTTP server shutdown failed", "error", err)
		} else {
			log.Printf("HTTP server shutdown complete")
			slog.Info("HTTP server shutdown complete")
		}
	}

	// Close analytics (flushes write queue and closes database)
	if p.analytics != nil {
		p.analytics.Close()
		slog.Info("Analytics writer closed")
	}

	log.Printf("Proxy shutdown complete")
	slog.Info("Proxy shutdown complete")
}

// handleProxy processes and forwards requests
func (p *Proxy) handleProxy(w http.ResponseWriter, r *http.Request) {
	// Check if client already disconnected before processing
	select {
	case <-r.Context().Done():
		log.Printf("Client disconnected before proxy processing: %s", r.RemoteAddr)
		return
	default:
	}

	// Acquire semaphore slot for rate limiting
	select {
	case p.maxConcurrent <- struct{}{}:
		// Got a slot, continue processing
		defer func() { <-p.maxConcurrent }() // Release slot when done
	case <-r.Context().Done():
		// Client disconnected while waiting
		http.Error(w, "Request cancelled", http.StatusRequestTimeout)
		return
	}

	startTime := time.Now()

	// Parse request for metrics
	var body []byte
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		body, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}

	model, prompt, endpoint := p.parseRequest(r, body)
	promptCategory := p.metrics.categorizer.Categorize(prompt)

	// Track active requests
	p.metrics.activeRequests.Inc()
	defer p.metrics.activeRequests.Dec()

	// Log the request with client IP
	clientIP := r.RemoteAddr
	if xForwardedFor := r.Header.Get("X-Forwarded-For"); xForwardedFor != "" {
		clientIP = xForwardedFor + " (via " + r.RemoteAddr + ")"
	}
	log.Printf("[%s] Proxying %s %s to %s%s (model: %s, category: %s)", 
		clientIP, r.Method, r.URL.Path, p.target, r.URL.Path, model, promptCategory)

	// Create context for metrics collection
	ctx := &ProxyContext{
		StartTime:      startTime,
		Model:          model,
		Prompt:         prompt,
		Endpoint:       endpoint,
		PromptCategory: promptCategory,
		Writer:         w,
		Request:        r,
		ClientIP:       clientIP,
	}

	// Store context for response processing
	r = r.WithContext(withProxyContext(r.Context(), ctx))

	// Create a response writer wrapper to ensure flushing
	wrapped := &responseWriterWrapper{
		ResponseWriter: w,
		serviceMode:    IsRunningAsService(),
	}
	
	// Forward the request
	p.reverseProxy.ServeHTTP(wrapped, r)
	
	// Ensure final flush in service mode
	if wrapped.serviceMode {
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

// modifyResponse intercepts and modifies the response for metrics
func (p *Proxy) modifyResponse(resp *http.Response) error {
	// Log response received from upstream
	if IsRunningAsService() {
		LogPrintf("modifyResponse: Got response %d from upstream for %s", resp.StatusCode, resp.Request.URL.Path)
	}
	
	ctx := getProxyContext(resp.Request.Context())
	if ctx == nil {
		if IsRunningAsService() {
			LogPrintf("WARNING: No proxy context found for response")
		}
		return nil
	}

	// For streaming responses, we need to wrap the body
	if strings.Contains(resp.Header.Get("Content-Type"), "application/x-ndjson") ||
		strings.Contains(resp.Request.URL.Path, "/generate") ||
		strings.Contains(resp.Request.URL.Path, "/chat") {
		
		// Wrap the response body for streaming metrics collection
		resp.Body = &streamingResponseBody{
			ReadCloser: resp.Body,
			proxy:      p,
			ctx:        ctx,
		}
	} else {
		// For non-streaming responses, read and process
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			
			// Extract metrics from response
			p.processNonStreamingResponse(ctx, body, resp.StatusCode)
		}
	}

	return nil
}

// errorHandler handles proxy errors
func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	ctx := getProxyContext(r.Context())
	if ctx != nil {
		duration := time.Since(ctx.StartTime).Seconds()
		p.recordMetrics(ctx, duration, 0, 0, 500, err.Error())
	}

	clientIP := "unknown"
	if ctx != nil && ctx.ClientIP != "" {
		clientIP = ctx.ClientIP
	} else {
		clientIP = r.RemoteAddr
	}
	log.Printf("[%s] Proxy error for %s %s: %v", clientIP, r.Method, r.URL.Path, err)
	http.Error(w, fmt.Sprintf("Proxy error: %v", err), http.StatusBadGateway)
}

// parseRequest extracts model, prompt, and endpoint from request
func (p *Proxy) parseRequest(r *http.Request, body []byte) (model, prompt, endpoint string) {
	model = "unknown"
	prompt = ""
	endpoint = strings.TrimPrefix(r.URL.Path, "/")

	if len(body) > 0 {
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err == nil {
			if m, ok := data["model"].(string); ok {
				model = m
			}
			if p, ok := data["prompt"].(string); ok {
				prompt = p
			} else if messages, ok := data["messages"].([]interface{}); ok && len(messages) > 0 {
				// Extract prompt from messages array - bounds check already done above
				if lastMsg, ok := messages[len(messages)-1].(map[string]interface{}); ok {
					if content, ok := lastMsg["content"].(string); ok {
						prompt = content
					}
				}
			}
		}
	}

	// Clean up endpoint
	if strings.HasPrefix(endpoint, "api/") {
		endpoint = strings.TrimPrefix(endpoint, "api/")
	}

	return model, prompt, endpoint
}

// processNonStreamingResponse handles metrics for non-streaming responses
func (p *Proxy) processNonStreamingResponse(ctx *ProxyContext, body []byte, statusCode int) {
	duration := time.Since(ctx.StartTime).Seconds()
	
	// Extract detailed metrics from response
	tokens := 0
	promptTokens := 0
	tokensPerSecond := 0.0
	
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err == nil {
		// Extract generated tokens
		if evalCount, ok := data["eval_count"].(float64); ok {
			tokens = int(evalCount)
			
			// Calculate tokens per second from Ollama's eval_duration
			if evalDuration, ok := data["eval_duration"].(float64); ok && evalDuration > 0 {
				tokensPerSecond = evalCount / (evalDuration / 1e9) // Convert nanoseconds to seconds
			}
		}
		
		// Extract prompt tokens
		if promptEvalCount, ok := data["prompt_eval_count"].(float64); ok {
			promptTokens = int(promptEvalCount)
		}
		
		// Store additional metrics in context for analytics
		ctx.PromptTokens = promptTokens
		if loadDuration, ok := data["load_duration"].(float64); ok {
			ctx.LoadDuration = loadDuration / 1e9
		}
		if totalDuration, ok := data["total_duration"].(float64); ok {
			ctx.TotalDuration = totalDuration / 1e9
		}
		
		// Extract response content for preview
		if response, ok := data["response"].(string); ok {
			ctx.ResponsePreview = truncate(response, 200)
		} else if message, ok := data["message"].(map[string]interface{}); ok {
			if content, ok := message["content"].(string); ok {
				ctx.ResponsePreview = truncate(content, 200)
			}
		}
	}

	p.recordMetrics(ctx, duration, tokens, tokensPerSecond, statusCode, "")
}

// recordMetrics records both Prometheus metrics and analytics
func (p *Proxy) recordMetrics(ctx *ProxyContext, duration float64, tokens int, tokensPerSecond float64, statusCode int, errorMsg string) {
	// Update Prometheus metrics (client_ip removed from labels to prevent cardinality explosion)
	// Client IP is still tracked in analytics SQLite database for detailed analysis
	p.metrics.requestDuration.WithLabelValues(ctx.Model, ctx.Endpoint, ctx.PromptCategory).Observe(duration)

	status := "success"
	if statusCode >= 400 {
		status = "error"
	} else if errorMsg != "" {
		status = "error"
	}
	p.metrics.requestsTotal.WithLabelValues(ctx.Model, ctx.Endpoint, ctx.PromptCategory, status).Inc()

	if tokens > 0 {
		p.metrics.tokensGenerated.WithLabelValues(ctx.Model, ctx.PromptCategory).Observe(float64(tokens))
		if tokensPerSecond > 0 {
			p.metrics.tokensPerSecond.WithLabelValues(ctx.Model, ctx.PromptCategory).Observe(tokensPerSecond)
		}
	}

	// Record analytics
	record := AnalyticsRecord{
		Timestamp:        time.Now(),
		Model:            ctx.Model,
		Endpoint:         ctx.Endpoint,
		Prompt:           ctx.Prompt,
		PromptCategory:   ctx.PromptCategory,
		ResponsePreview:  ctx.ResponsePreview,
		DurationSeconds:  duration,
		TokensGenerated:  tokens,
		TokensPerSecond:  tokensPerSecond,
		StatusCode:       statusCode,
		ErrorMessage:     errorMsg,
		ClientIP:         ctx.ClientIP,
		UserAgent:        ctx.Request.Header.Get("User-Agent"),
		PromptTokens:     ctx.PromptTokens,
		LoadDuration:     ctx.LoadDuration,
		TotalDuration:    ctx.TotalDuration,
		User:             "anonymous", // Default user
		Status:           status,
		QueueTime:        0, // Could be calculated if we track queue start time
		TimeToFirstToken: ctx.TimeToFirstToken,
		Metadata:         map[string]interface{}{"endpoint": ctx.Endpoint},
	}
	
	p.analytics.Record(record)

	log.Printf("[%s] %s/%s - %.2fs - %d tokens - %d", ctx.ClientIP, ctx.Model, ctx.PromptCategory, duration, tokens, statusCode)
}

// handleMetrics serves Prometheus metrics
func (p *Proxy) handleMetrics(w http.ResponseWriter, r *http.Request) {
	p.metrics.Handler().ServeHTTP(w, r)
}

// handleTest provides a test endpoint
func (p *Proxy) handleTest(w http.ResponseWriter, r *http.Request) {
	// Log the test request
	clientIP := r.RemoteAddr
	if xForwardedFor := r.Header.Get("X-Forwarded-For"); xForwardedFor != "" {
		clientIP = xForwardedFor + " (via " + r.RemoteAddr + ")"
	}
	log.Printf("[%s] Test endpoint accessed", clientIP)
	
	// Test connectivity to Ollama
	resp, err := http.Get(p.target.String() + "/api/tags")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           "error",
			"ollama_host":      p.target.String(),
			"ollama_reachable": false,
			"error":            err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&data)

	models := []string{}
	if modelList, ok := data["models"].([]interface{}); ok {
		for _, m := range modelList {
			if model, ok := m.(map[string]interface{}); ok {
				if name, ok := model["name"].(string); ok {
					models = append(models, name)
				}
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "ok",
		"ollama_host":      p.target.String(),
		"ollama_reachable": true,
		"models":           models,
	})
}

// responseWriterWrapper ensures proper flushing in service mode
type responseWriterWrapper struct {
	http.ResponseWriter
	serviceMode bool
}

func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	// In service mode, flush after each write for streaming responses
	if w.serviceMode && n > 0 {
		if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	return n, err
}

// streamingResponseBody wraps the response body for streaming metrics collection
type streamingResponseBody struct {
	io.ReadCloser
	proxy           *Proxy
	ctx             *ProxyContext
	accumulated     []byte
	tokens          int
	responseText    strings.Builder
	firstTokenTime  time.Time
	metricsData     map[string]interface{}
	metricsRecorded bool // Prevents double-recording on early close
}

func (s *streamingResponseBody) Read(p []byte) (n int, err error) {
	n, err = s.ReadCloser.Read(p)

	if n > 0 {
		// Accumulate data for metrics (limit to 1MB to prevent memory issues)
		if len(s.accumulated) < 1024*1024 {
			s.accumulated = append(s.accumulated, p[:n]...)
		}

		// Parse NDJSON chunks
		lines := strings.Split(string(p[:n]), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}

			var data map[string]interface{}
			if err := json.Unmarshal([]byte(line), &data); err == nil {
				// Extract response text
				if response, ok := data["response"].(string); ok {
					if s.firstTokenTime.IsZero() && response != "" {
						s.firstTokenTime = time.Now()
						s.ctx.TimeToFirstToken = s.firstTokenTime.Sub(s.ctx.StartTime).Seconds()
					}
					s.responseText.WriteString(response)
				}

				// Store metrics data from the final chunk
				if done, ok := data["done"].(bool); ok && done {
					s.metricsData = data
				}
			}
		}
	}

	// When stream ends, record metrics
	if err == io.EOF {
		s.recordStreamMetrics()
	}

	return n, err
}

// Close ensures metrics are recorded even on early connection close
func (s *streamingResponseBody) Close() error {
	// Record metrics if not already done (handles early disconnect)
	if !s.metricsRecorded {
		s.recordStreamMetrics()
	}
	return s.ReadCloser.Close()
}

// recordStreamMetrics extracts and records metrics from streaming response
func (s *streamingResponseBody) recordStreamMetrics() {
	// Prevent double-recording
	if s.metricsRecorded {
		return
	}
	s.metricsRecorded = true

	duration := time.Since(s.ctx.StartTime).Seconds()
	tokensPerSecond := 0.0
	tokens := 0

	// Extract metrics from final data
	if s.metricsData != nil {
		if evalCount, ok := s.metricsData["eval_count"].(float64); ok {
			tokens = int(evalCount)
			if evalDuration, ok := s.metricsData["eval_duration"].(float64); ok && evalDuration > 0 {
				tokensPerSecond = evalCount / (evalDuration / 1e9)
			}
		}

		if promptEvalCount, ok := s.metricsData["prompt_eval_count"].(float64); ok {
			s.ctx.PromptTokens = int(promptEvalCount)
		}

		if loadDuration, ok := s.metricsData["load_duration"].(float64); ok {
			s.ctx.LoadDuration = loadDuration / 1e9
		}

		if totalDuration, ok := s.metricsData["total_duration"].(float64); ok {
			s.ctx.TotalDuration = totalDuration / 1e9
		}
	}

	// Store response preview
	s.ctx.ResponsePreview = truncate(s.responseText.String(), 200)

	s.proxy.recordMetrics(s.ctx, duration, tokens, tokensPerSecond, 200, "")
}