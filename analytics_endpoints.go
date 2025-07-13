package main

import (
	"encoding/json"
	"net/http"
	"sort"
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
	
	// Get all records in time range
	query := `SELECT model, client_ip, duration_seconds, tokens_generated, prompt_tokens, 
	          status_code, timestamp FROM interactions 
	          WHERE timestamp >= ? ORDER BY timestamp ASC`
	
	rows, err := p.analytics.db.Query(query, startTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	
	// Process records for analytics
	ipMap := make(map[string]*IPStat)
	modelMap := make(map[string]*ModelStat)
	var allRecords []struct {
		model, ip string
		latency, inputTokens, outputTokens float64
		statusCode int
		timestamp time.Time
	}
	
	var totalLatency, totalInputTokens, totalOutputTokens float64
	var successCount, totalCount int
	
	for rows.Next() {
		var model, ip string
		var latency, outputTokens, inputTokens float64
		var statusCode int
		var timestamp time.Time
		
		err := rows.Scan(&model, &ip, &latency, &outputTokens, &inputTokens, &statusCode, &timestamp)
		if err != nil {
			continue
		}
		
		totalCount++
		totalLatency += latency
		totalInputTokens += inputTokens
		totalOutputTokens += outputTokens
		
		if statusCode < 400 {
			successCount++
		}
		
		// Track by IP
		if ipStat, exists := ipMap[ip]; exists {
			ipStat.RequestCount++
			ipStat.AvgLatency = (ipStat.AvgLatency*float64(ipStat.RequestCount-1) + latency*1000) / float64(ipStat.RequestCount)
			ipStat.TotalTokens += int(outputTokens)
		} else {
			ipMap[ip] = &IPStat{
				IP:           ip,
				RequestCount: 1,
				AvgLatency:   latency * 1000, // Convert to ms
				TotalTokens:  int(outputTokens),
			}
		}
		
		// Track by model
		if modelStat, exists := modelMap[model]; exists {
			modelStat.RequestCount++
			modelStat.AvgLatency = (modelStat.AvgLatency*float64(modelStat.RequestCount-1) + latency*1000) / float64(modelStat.RequestCount)
			modelStat.TotalTokens += int(outputTokens)
		} else {
			modelMap[model] = &ModelStat{
				Model:        model,
				RequestCount: 1,
				AvgLatency:   latency * 1000,
				TotalTokens:  int(outputTokens),
			}
		}
		
		allRecords = append(allRecords, struct {
			model, ip string
			latency, inputTokens, outputTokens float64
			statusCode int
			timestamp time.Time
		}{model, ip, latency, inputTokens, outputTokens, statusCode, timestamp})
	}
	
	// Calculate basic stats
	stats.TotalRequests = totalCount
	stats.UniqueIPs = len(ipMap)
	stats.UniqueModels = len(modelMap)
	
	if totalCount > 0 {
		stats.AvgResponseTime = (totalLatency / float64(totalCount)) * 1000 // Convert to ms
		stats.AvgInputTokens = totalInputTokens / float64(totalCount)
		stats.AvgOutputTokens = totalOutputTokens / float64(totalCount)
		stats.SuccessRate = (float64(successCount) / float64(totalCount)) * 100
		stats.ErrorRate = 100 - stats.SuccessRate
		
		// Calculate requests per minute
		if hours > 0 {
			totalMinutes := float64(hours * 60)
			stats.RequestsPerMinute = float64(totalCount) / totalMinutes
		}
		
		// Calculate average tokens per second
		totalTokensPerSecond := 0.0
		validCount := 0
		for _, record := range allRecords {
			if record.latency > 0 && record.outputTokens > 0 {
				tokensPerSec := record.outputTokens / record.latency
				totalTokensPerSecond += tokensPerSec
				validCount++
			}
		}
		if validCount > 0 {
			stats.AvgTokensPerSec = totalTokensPerSecond / float64(validCount)
		}
	}
	
	// Sort and get top IPs
	var ipStats []IPStat
	for _, stat := range ipMap {
		ipStats = append(ipStats, *stat)
	}
	sort.Slice(ipStats, func(i, j int) bool {
		return ipStats[i].RequestCount > ipStats[j].RequestCount
	})
	if len(ipStats) > 10 {
		ipStats = ipStats[:10]
	}
	stats.TopIPs = ipStats
	
	// Sort and get top models
	var modelStats []ModelStat
	for _, stat := range modelMap {
		modelStats = append(modelStats, *stat)
	}
	sort.Slice(modelStats, func(i, j int) bool {
		return modelStats[i].RequestCount > modelStats[j].RequestCount
	})
	if len(modelStats) > 10 {
		modelStats = modelStats[:10]
	}
	stats.TopModels = modelStats
	
	// Generate recent trend (hourly buckets)
	trendMap := make(map[int64]*TrendPoint)
	for _, record := range allRecords {
		// Round to hour
		hour := record.timestamp.Truncate(time.Hour).Unix()
		if point, exists := trendMap[hour]; exists {
			point.RequestCount++
			point.AvgLatency = (point.AvgLatency*float64(point.RequestCount-1) + record.latency*1000) / float64(point.RequestCount)
		} else {
			trendMap[hour] = &TrendPoint{
				Timestamp:    hour,
				RequestCount: 1,
				AvgLatency:   record.latency * 1000,
			}
		}
	}
	
	var trendPoints []TrendPoint
	for _, point := range trendMap {
		trendPoints = append(trendPoints, *point)
	}
	sort.Slice(trendPoints, func(i, j int) bool {
		return trendPoints[i].Timestamp < trendPoints[j].Timestamp
	})
	stats.RecentTrend = trendPoints
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}