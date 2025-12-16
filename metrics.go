package main

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const MaxPromptCategories = 50

// MetricsCollector handles Prometheus metrics collection
type MetricsCollector struct {
	requestDuration *prometheus.HistogramVec
	tokensGenerated *prometheus.HistogramVec
	tokensPerSecond *prometheus.HistogramVec
	requestsTotal   *prometheus.CounterVec
	activeRequests  prometheus.Gauge
	categorizer     *PromptCategorizer
	registry        *prometheus.Registry
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	registry := prometheus.NewRegistry()

	mc := &MetricsCollector{
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ollama_request_duration_seconds",
				Help:    "Request duration distribution",
				Buckets: []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0},
			},
			[]string{"model", "endpoint", "prompt_category"},  // Removed client_ip for cardinality control
		),
		tokensGenerated: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ollama_tokens_generated",
				Help:    "Distribution of tokens generated",
				Buckets: []float64{10, 50, 100, 250, 500, 1000, 2000, 5000},
			},
			[]string{"model", "prompt_category"},  // Removed client_ip for cardinality control
		),
		tokensPerSecond: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ollama_tokens_per_second",
				Help:    "Distribution of token generation speed",
				Buckets: []float64{1, 5, 10, 20, 30, 50, 75, 100, 150, 200},
			},
			[]string{"model", "prompt_category"},  // Removed client_ip for cardinality control
		),
		requestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ollama_requests_total",
				Help: "Total number of requests",
			},
			[]string{"model", "endpoint", "prompt_category", "status"},  // Removed client_ip for cardinality control
		),
		activeRequests: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "ollama_active_requests",
				Help: "Currently active requests",
			},
		),
		categorizer: NewPromptCategorizer(),
		registry:    registry,
	}

	// Register metrics
	registry.MustRegister(
		mc.requestDuration,
		mc.tokensGenerated,
		mc.tokensPerSecond,
		mc.requestsTotal,
		mc.activeRequests,
	)

	// Also register Go runtime metrics
	registry.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)

	return mc
}

// Handler returns the HTTP handler for metrics
func (mc *MetricsCollector) Handler() http.Handler {
	return promhttp.HandlerFor(mc.registry, promhttp.HandlerOpts{})
}

// PromptCategorizer categorizes prompts to limit metric cardinality
type PromptCategorizer struct {
	patterns   []patternCategory
	categories map[string]bool
	mu         sync.RWMutex
}

type patternCategory struct {
	pattern  *regexp.Regexp
	category string
}

// NewPromptCategorizer creates a new prompt categorizer
func NewPromptCategorizer() *PromptCategorizer {
	pc := &PromptCategorizer{
		categories: make(map[string]bool),
	}

	// Define categorization patterns
	patterns := []struct {
		pattern  string
		category string
	}{
		{`(?i)summar`, "summarize"},
		{`(?i)translat`, "translate"},
		{`(?i)explain`, "explain"},
		{`(?i)write.*code`, "code_write"},
		{`(?i)debug|fix`, "code_debug"},
		{`(?i)question|what|how|why|when`, "question"},
		{`(?i)creat|generat`, "creative"},
		{`(?i)analyz|analy`, "analyze"},
		{`(?i)help`, "help"},
		{`(?i)list|enumerate`, "list"},
	}

	for _, p := range patterns {
		re, err := regexp.Compile(p.pattern)
		if err == nil {
			pc.patterns = append(pc.patterns, patternCategory{
				pattern:  re,
				category: p.category,
			})
		}
	}

	return pc
}

// Categorize returns a category for the given prompt
func (pc *PromptCategorizer) Categorize(prompt string) string {
	if prompt == "" {
		return "empty"
	}

	promptLower := strings.ToLower(prompt)

	// Check patterns
	for _, p := range pc.patterns {
		if p.pattern.MatchString(promptLower) {
			return p.category
		}
	}

	// Use first word as category if under limit
	words := strings.Fields(prompt)
	if len(words) > 0 {
		firstWord := strings.ToLower(words[0])
		
		pc.mu.RLock()
		count := len(pc.categories)
		pc.mu.RUnlock()

		if count < MaxPromptCategories {
			pc.mu.Lock()
			if len(pc.categories) < MaxPromptCategories {
				pc.categories[firstWord] = true
				pc.mu.Unlock()
				return firstWord
			}
			pc.mu.Unlock()
		}
	}

	// Fallback to hash-based category
	hash := md5.Sum([]byte(promptLower))
	return fmt.Sprintf("other_%x", hash[:4])
}