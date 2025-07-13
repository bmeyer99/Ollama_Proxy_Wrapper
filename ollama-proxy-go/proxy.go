package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Proxy handles HTTP reverse proxy with metrics collection
type Proxy struct {
	target       *url.URL
	reverseProxy *httputil.ReverseProxy
	port         int
	metrics      *MetricsCollector
	analytics    *AnalyticsWriter
}

// NewProxy creates a new proxy instance
func NewProxy(targetURL string, port int) *Proxy {
	target, err := url.Parse(targetURL)
	if err != nil {
		log.Fatalf("Invalid target URL: %v", err)
	}

	p := &Proxy{
		target:    target,
		port:      port,
		metrics:   NewMetricsCollector(),
		analytics: NewAnalyticsWriter("sqlite", filepath.Join(".", "ollama_analytics")),
	}

	// Create custom transport with proper timeouts for Ollama
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true, // Let Ollama handle compression
		ForceAttemptHTTP2:   false,
	}

	// Create reverse proxy with custom director
	p.reverseProxy = &httputil.ReverseProxy{
		Transport: transport,
		FlushInterval: -1, // Flush immediately for streaming
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

	log.Printf("Starting Ollama Proxy on port %d", p.port)
	log.Printf("Proxying to Ollama at %s", p.target)
	log.Printf("Metrics: http://localhost:%d/metrics", p.port)
	log.Printf("Analytics Dashboard: http://localhost:%d/analytics", p.port)

	return http.ListenAndServe(fmt.Sprintf(":%d", p.port), mux)
}

// Shutdown gracefully shuts down the proxy
func (p *Proxy) Shutdown() {
	log.Printf("Shutting down proxy...")
	if p.analytics != nil {
		p.analytics.Close()
	}
	log.Printf("Proxy shutdown complete")
}

// handleProxy processes and forwards requests
func (p *Proxy) handleProxy(w http.ResponseWriter, r *http.Request) {
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

	// Forward the request
	p.reverseProxy.ServeHTTP(w, r)
}

// modifyResponse intercepts and modifies the response for metrics
func (p *Proxy) modifyResponse(resp *http.Response) error {
	ctx := getProxyContext(resp.Request.Context())
	if ctx == nil {
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
	// Update Prometheus metrics
	p.metrics.requestDuration.WithLabelValues(ctx.Model, ctx.Endpoint, ctx.PromptCategory, ctx.ClientIP).Observe(duration)
	
	status := "success"
	if statusCode >= 400 {
		status = "error"
	} else if errorMsg != "" {
		status = "error"
	}
	p.metrics.requestsTotal.WithLabelValues(ctx.Model, ctx.Endpoint, ctx.PromptCategory, status, ctx.ClientIP).Inc()

	if tokens > 0 {
		p.metrics.tokensGenerated.WithLabelValues(ctx.Model, ctx.PromptCategory, ctx.ClientIP).Observe(float64(tokens))
		if tokensPerSecond > 0 {
			p.metrics.tokensPerSecond.WithLabelValues(ctx.Model, ctx.PromptCategory, ctx.ClientIP).Observe(tokensPerSecond)
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

// streamingResponseBody wraps the response body for streaming metrics collection
type streamingResponseBody struct {
	io.ReadCloser
	proxy          *Proxy
	ctx            *ProxyContext
	accumulated    []byte
	tokens         int
	responseText   strings.Builder
	firstTokenTime time.Time
	metricsData    map[string]interface{}
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
	
	return n, err
}