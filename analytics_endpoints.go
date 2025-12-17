package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// AnalyticsStats represents useful analytics statistics
type AnalyticsStats struct {
	// Basic counts
	TotalRequests    int     `json:"total_requests"`
	UniqueIPs        int     `json:"unique_ips"`
	UniqueModels     int     `json:"unique_models"`
	
	// Performance metrics  
	AvgResponseTime  float64 `json:"avg_response_time_ms"`
	AvgInputTokens   float64 `json:"avg_input_tokens"`
	AvgOutputTokens  float64 `json:"avg_output_tokens"`
	AvgTokensPerSec  float64 `json:"avg_tokens_per_second"`
	
	// Rate metrics
	RequestsPerMinute float64 `json:"requests_per_minute"`
	SuccessRate       float64 `json:"success_rate_percent"`
	ErrorRate         float64 `json:"error_rate_percent"`
	
	// Top lists
	TopIPs       []IPStat    `json:"top_ips"`
	TopModels    []ModelStat `json:"top_models"`
	RecentTrend  []TrendPoint `json:"recent_trend"`
	
	// Time range info
	TimeRangeHours int    `json:"time_range_hours"`
	DataStartTime  string `json:"data_start_time"`
	DataEndTime    string `json:"data_end_time"`
}

type IPStat struct {
	IP           string  `json:"ip"`
	RequestCount int     `json:"request_count"`
	AvgLatency   float64 `json:"avg_latency_ms"`
	TotalTokens  int     `json:"total_tokens"`
}

type ModelStat struct {
	Model        string  `json:"model"`
	RequestCount int     `json:"request_count"`
	AvgLatency   float64 `json:"avg_latency_ms"`
	TotalTokens  int     `json:"total_tokens"`
}

type TrendPoint struct {
	Timestamp    int64 `json:"timestamp"`
	RequestCount int   `json:"request_count"`
	AvgLatency   float64 `json:"avg_latency"`
}

// Enhanced analytics stats endpoint
func (p *Proxy) handleAnalyticsStatsEnhanced(w http.ResponseWriter, r *http.Request) {
	if p.analytics.backend != "sqlite" || p.analytics.db == nil {
		http.Error(w, "Analytics not available", http.StatusServiceUnavailable)
		return
	}

	// Get time range (default last 24 hours)
	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 {
			hours = parsed
		}
	}

	startTime := time.Now().Add(-time.Duration(hours) * time.Hour)

	stats := &AnalyticsStats{
		TimeRangeHours: hours,
		DataStartTime:  startTime.Format(time.RFC3339),
		DataEndTime:    time.Now().Format(time.RFC3339),
	}

	// Use SQL aggregations for better performance (no in-memory processing)
	// Get basic aggregate statistics
	basicStatsQuery := `
		SELECT
			COUNT(*) as total_requests,
			COUNT(DISTINCT client_ip) as unique_ips,
			COUNT(DISTINCT model) as unique_models,
			AVG(duration_seconds * 1000) as avg_response_time_ms,
			AVG(prompt_tokens) as avg_input_tokens,
			AVG(tokens_generated) as avg_output_tokens,
			AVG(CASE WHEN duration_seconds > 0 AND tokens_generated > 0
			    THEN tokens_generated / duration_seconds ELSE 0 END) as avg_tokens_per_sec,
			SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END) * 100.0 / COUNT(*) as success_rate
		FROM interactions
		WHERE timestamp >= ?
	`

	err := p.analytics.db.QueryRow(basicStatsQuery, startTime).Scan(
		&stats.TotalRequests,
		&stats.UniqueIPs,
		&stats.UniqueModels,
		&stats.AvgResponseTime,
		&stats.AvgInputTokens,
		&stats.AvgOutputTokens,
		&stats.AvgTokensPerSec,
		&stats.SuccessRate,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stats.ErrorRate = 100 - stats.SuccessRate

	// Calculate requests per minute
	if hours > 0 {
		totalMinutes := float64(hours * 60)
		stats.RequestsPerMinute = float64(stats.TotalRequests) / totalMinutes
	}

	// Get top IPs using SQL aggregation
	topIPsQuery := `
		SELECT
			client_ip,
			COUNT(*) as request_count,
			AVG(duration_seconds * 1000) as avg_latency_ms,
			SUM(tokens_generated) as total_tokens
		FROM interactions
		WHERE timestamp >= ?
		GROUP BY client_ip
		ORDER BY request_count DESC
		LIMIT 10
	`

	rows, err := p.analytics.db.Query(topIPsQuery, startTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var ipStats []IPStat
	for rows.Next() {
		var stat IPStat
		if err := rows.Scan(&stat.IP, &stat.RequestCount, &stat.AvgLatency, &stat.TotalTokens); err == nil {
			ipStats = append(ipStats, stat)
		}
	}
	stats.TopIPs = ipStats

	// Get top models using SQL aggregation
	topModelsQuery := `
		SELECT
			model,
			COUNT(*) as request_count,
			AVG(duration_seconds * 1000) as avg_latency_ms,
			SUM(tokens_generated) as total_tokens
		FROM interactions
		WHERE timestamp >= ?
		GROUP BY model
		ORDER BY request_count DESC
		LIMIT 10
	`

	rows, err = p.analytics.db.Query(topModelsQuery, startTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var modelStats []ModelStat
	for rows.Next() {
		var stat ModelStat
		if err := rows.Scan(&stat.Model, &stat.RequestCount, &stat.AvgLatency, &stat.TotalTokens); err == nil {
			modelStats = append(modelStats, stat)
		}
	}
	stats.TopModels = modelStats

	// Get hourly trend using SQL aggregation
	trendQuery := `
		SELECT
			strftime('%s', datetime(timestamp, 'unixepoch', 'start of hour')) as hour_timestamp,
			COUNT(*) as request_count,
			AVG(duration_seconds * 1000) as avg_latency
		FROM interactions
		WHERE timestamp >= ?
		GROUP BY hour_timestamp
		ORDER BY hour_timestamp ASC
	`

	rows, err = p.analytics.db.Query(trendQuery, startTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var trendPoints []TrendPoint
	for rows.Next() {
		var point TrendPoint
		if err := rows.Scan(&point.Timestamp, &point.RequestCount, &point.AvgLatency); err == nil {
			trendPoints = append(trendPoints, point)
		}
	}
	stats.RecentTrend = trendPoints

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}