package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// AnalyticsRecord represents a single analytics entry
type AnalyticsRecord struct {
	ID               int64     `json:"id"`
	Timestamp        time.Time `json:"timestamp"`
	Model            string    `json:"model"`
	Endpoint         string    `json:"endpoint"`
	Prompt           string    `json:"prompt"`
	PromptCategory   string    `json:"category"`
	ResponsePreview  string    `json:"response"`
	DurationSeconds  float64   `json:"latency"`
	TokensGenerated  int       `json:"output_tokens"`
	TokensPerSecond  float64   `json:"tokens_per_second"`
	PromptTokens     int       `json:"input_tokens"`
	LoadDuration     float64   `json:"load_duration"`
	TotalDuration    float64   `json:"total_duration"`
	StatusCode       int       `json:"status_code"`
	ErrorMessage     string    `json:"error"`
	UserAgent        string    `json:"user_agent"`
	ClientIP         string    `json:"client_ip"`
	User             string    `json:"user"`
	Cost             float64   `json:"cost"`
	Status           string    `json:"status"`
	QueueTime        float64   `json:"queue_time"`
	TimeToFirstToken float64   `json:"time_to_first_token"`
	Metadata         map[string]interface{} `json:"metadata"`
}

// MarshalJSON customizes JSON serialization for Unix timestamps
func (a AnalyticsRecord) MarshalJSON() ([]byte, error) {
	type Alias AnalyticsRecord
	return json.Marshal(&struct {
		Alias
		Timestamp int64 `json:"timestamp"`
	}{
		Alias:     (Alias)(a),
		Timestamp: a.Timestamp.Unix(),
	})
}

// AnalyticsWriter handles writing analytics to storage
type AnalyticsWriter struct {
	backend    string
	dataDir    string
	db         *sql.DB
	writeQueue chan AnalyticsRecord
	wg         sync.WaitGroup
	mu         sync.RWMutex
	shutdown   chan bool
}

// NewAnalyticsWriter creates a new analytics writer
func NewAnalyticsWriter(backend, dataDir string) *AnalyticsWriter {
	// Ensure data directory exists
	os.MkdirAll(dataDir, 0755)
	
	aw := &AnalyticsWriter{
		backend:    backend,
		dataDir:    dataDir,
		writeQueue: make(chan AnalyticsRecord, 1000),
		shutdown:   make(chan bool),
	}

	if backend == "sqlite" {
		if err := aw.initSQLite(); err != nil {
			log.Printf("Failed to initialize SQLite: %v", err)
			return aw
		}
	}

	// Start writer goroutine
	aw.wg.Add(1)
	go aw.writerLoop()

	// Start cleanup goroutine
	aw.wg.Add(1)
	go func() {
		defer aw.wg.Done()
		aw.cleanupLoop()
	}()

	return aw
}

// initSQLite initializes the SQLite database
func (aw *AnalyticsWriter) initSQLite() error {
	dbPath := filepath.Join(aw.dataDir, "ollama_analytics.db")

	// Add WAL mode and timeout to connection string for better concurrency
	connStr := dbPath + "?_journal=WAL&_timeout=5000&_busy_timeout=5000"
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// CRITICAL: SQLite is single-writer, configure connection pool accordingly
	// This prevents SQLITE_BUSY errors and improves reliability
	db.SetMaxOpenConns(1)     // Single writer for SQLite
	db.SetMaxIdleConns(1)     // Keep connection alive
	db.SetConnMaxLifetime(0)  // Reuse connections indefinitely

	// Create table
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS interactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		model TEXT,
		endpoint TEXT,
		prompt TEXT,
		prompt_category TEXT,
		response_preview TEXT,
		duration_seconds REAL,
		tokens_generated INTEGER,
		tokens_per_second REAL,
		prompt_tokens INTEGER,
		load_duration REAL,
		total_duration REAL,
		status_code INTEGER,
		error_message TEXT,
		user_agent TEXT,
		client_ip TEXT,
		user TEXT,
		cost REAL,
		status TEXT,
		queue_time REAL,
		time_to_first_token REAL,
		metadata TEXT
	);`

	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Add missing columns for existing databases (migration)
	migrations := []string{
		"ALTER TABLE interactions ADD COLUMN prompt_tokens INTEGER DEFAULT 0;",
		"ALTER TABLE interactions ADD COLUMN load_duration REAL DEFAULT 0;",
		"ALTER TABLE interactions ADD COLUMN total_duration REAL DEFAULT 0;",
		"ALTER TABLE interactions ADD COLUMN user TEXT DEFAULT '';",
		"ALTER TABLE interactions ADD COLUMN cost REAL DEFAULT 0;",
		"ALTER TABLE interactions ADD COLUMN status TEXT DEFAULT 'success';",
		"ALTER TABLE interactions ADD COLUMN queue_time REAL DEFAULT 0;",
		"ALTER TABLE interactions ADD COLUMN time_to_first_token REAL DEFAULT 0;",
		"ALTER TABLE interactions ADD COLUMN metadata TEXT DEFAULT '{}';",
	}

	for _, migration := range migrations {
		// Ignore errors - columns might already exist
		db.Exec(migration)
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_timestamp ON interactions(timestamp);",
		"CREATE INDEX IF NOT EXISTS idx_model ON interactions(model);",
		"CREATE INDEX IF NOT EXISTS idx_prompt_category ON interactions(prompt_category);",
	}

	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			log.Printf("Failed to create index: %v", err)
		}
	}

	aw.db = db
	return nil
}

// Record queues a record for writing
func (aw *AnalyticsWriter) Record(record AnalyticsRecord) {
	select {
	case aw.writeQueue <- record:
	default:
		log.Println("Analytics queue full, dropping record")
	}
}

// writerLoop processes the write queue
func (aw *AnalyticsWriter) writerLoop() {
	defer aw.wg.Done()

	for record := range aw.writeQueue {
		if aw.backend == "sqlite" && aw.db != nil {
			aw.writeSQLite(record)
		}
	}
}

// writeSQLite writes a record to SQLite
func (aw *AnalyticsWriter) writeSQLite(record AnalyticsRecord) {
	query := `
	INSERT INTO interactions (
		timestamp, model, endpoint, prompt, prompt_category,
		response_preview, duration_seconds, tokens_generated,
		tokens_per_second, prompt_tokens, load_duration, total_duration,
		status_code, error_message, user_agent, client_ip,
		user, cost, status, queue_time, time_to_first_token, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	// Serialize metadata to JSON
	metadataJSON := "{}"
	if record.Metadata != nil {
		if data, err := json.Marshal(record.Metadata); err == nil {
			metadataJSON = string(data)
		}
	}

	_, err := aw.db.Exec(query,
		record.Timestamp,
		record.Model,
		record.Endpoint,
		truncate(record.Prompt, 1000),
		record.PromptCategory,
		truncate(record.ResponsePreview, 200),
		record.DurationSeconds,
		record.TokensGenerated,
		record.TokensPerSecond,
		record.PromptTokens,
		record.LoadDuration,
		record.TotalDuration,
		record.StatusCode,
		record.ErrorMessage,
		record.UserAgent,
		record.ClientIP,
		record.User,
		record.Cost,
		record.Status,
		record.QueueTime,
		record.TimeToFirstToken,
		metadataJSON,
	)

	if err != nil {
		log.Printf("Failed to write analytics record: %v", err)
	}
}

// cleanupLoop periodically removes old data
func (aw *AnalyticsWriter) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if aw.backend == "sqlite" && aw.db != nil {
				cutoff := time.Now().AddDate(0, 0, -7) // 7 days retention
				query := "DELETE FROM interactions WHERE timestamp < ?"
				
				result, err := aw.db.Exec(query, cutoff)
				if err != nil {
					log.Printf("Cleanup error: %v", err)
					continue
				}
				
				if rows, _ := result.RowsAffected(); rows > 0 {
					log.Printf("Cleaned up %d old analytics records", rows)
				}
			}
		case <-aw.shutdown:
			return
		}
	}
}

// Search performs analytics search
func (aw *AnalyticsWriter) Search(params url.Values) ([]AnalyticsRecord, error) {
	if aw.backend != "sqlite" || aw.db == nil {
		return nil, fmt.Errorf("search only available with sqlite backend")
	}

	query := "SELECT id, timestamp, model, endpoint, prompt, prompt_category, response_preview, duration_seconds, tokens_generated, tokens_per_second, prompt_tokens, load_duration, total_duration, status_code, error_message, user_agent, client_ip, user, cost, status, queue_time, time_to_first_token, metadata FROM interactions WHERE 1=1"
	args := []interface{}{}

	// Build query conditions
	if model := params.Get("model"); model != "" {
		query += " AND model = ?"
		args = append(args, model)
	}

	// Support both 'search' and 'prompt_search' parameters
	search := params.Get("search")
	if search == "" {
		search = params.Get("prompt_search")
	}
	if search != "" {
		query += " AND prompt LIKE ?"
		args = append(args, "%"+search+"%")
	}

	if startTime := params.Get("start_time"); startTime != "" {
		if ts, err := strconv.ParseInt(startTime, 10, 64); err == nil {
			query += " AND timestamp >= ?"
			args = append(args, time.Unix(ts, 0))
		}
	}

	if endTime := params.Get("end_time"); endTime != "" {
		if ts, err := strconv.ParseInt(endTime, 10, 64); err == nil {
			query += " AND timestamp <= ?"
			args = append(args, time.Unix(ts, 0))
		}
	}

	// Add limit
	limit := 100
	if l := params.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	// Execute query
	rows, err := aw.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer rows.Close()

	results := make([]AnalyticsRecord, 0)
	for rows.Next() {
		var r AnalyticsRecord
		var metadataJSON string
		err := rows.Scan(
			&r.ID, &r.Timestamp, &r.Model, &r.Endpoint, &r.Prompt,
			&r.PromptCategory, &r.ResponsePreview, &r.DurationSeconds,
			&r.TokensGenerated, &r.TokensPerSecond, &r.PromptTokens,
			&r.LoadDuration, &r.TotalDuration, &r.StatusCode,
			&r.ErrorMessage, &r.UserAgent, &r.ClientIP,
			&r.User, &r.Cost, &r.Status, &r.QueueTime,
			&r.TimeToFirstToken, &metadataJSON,
		)
		if err != nil {
			log.Printf("Row scan error: %v", err)
			continue
		}
		
		// Parse metadata JSON
		if metadataJSON != "" && metadataJSON != "{}" {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(metadataJSON), &metadata); err == nil {
				r.Metadata = metadata
			}
		}
		results = append(results, r)
	}

	return results, nil
}

// GetStats returns analytics statistics
func (aw *AnalyticsWriter) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"backend":   aw.backend,
		"data_dir":  aw.dataDir,
		"queue_size": len(aw.writeQueue),
	}

	if aw.backend == "sqlite" && aw.db != nil {
		var count int
		if err := aw.db.QueryRow("SELECT COUNT(*) FROM interactions").Scan(&count); err == nil {
			stats["total_records"] = count
		}
	}

	return stats
}

// GetModels returns unique models from analytics
func (aw *AnalyticsWriter) GetModels() ([]string, error) {
	if aw.backend != "sqlite" || aw.db == nil {
		return []string{}, nil
	}
	
	rows, err := aw.db.Query("SELECT DISTINCT model FROM interactions WHERE model IS NOT NULL AND model != '' ORDER BY model")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var models []string
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err == nil {
			models = append(models, model)
		}
	}
	
	return models, nil
}

// GetMessageByID returns a single message by ID
func (aw *AnalyticsWriter) GetMessageByID(id int64) (*AnalyticsRecord, error) {
	if aw.backend != "sqlite" || aw.db == nil {
		return nil, fmt.Errorf("analytics not available")
	}
	
	query := "SELECT id, timestamp, model, endpoint, prompt, prompt_category, response_preview, duration_seconds, tokens_generated, tokens_per_second, prompt_tokens, load_duration, total_duration, status_code, error_message, user_agent, client_ip, user, cost, status, queue_time, time_to_first_token, metadata FROM interactions WHERE id = ?"
	
	var r AnalyticsRecord
	var metadataJSON string
	err := aw.db.QueryRow(query, id).Scan(
		&r.ID, &r.Timestamp, &r.Model, &r.Endpoint, &r.Prompt,
		&r.PromptCategory, &r.ResponsePreview, &r.DurationSeconds,
		&r.TokensGenerated, &r.TokensPerSecond, &r.PromptTokens,
		&r.LoadDuration, &r.TotalDuration, &r.StatusCode,
		&r.ErrorMessage, &r.UserAgent, &r.ClientIP,
		&r.User, &r.Cost, &r.Status, &r.QueueTime,
		&r.TimeToFirstToken, &metadataJSON,
	)
	if err != nil {
		return nil, err
	}
	
	// Parse metadata JSON
	if metadataJSON != "" && metadataJSON != "{}" {
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err == nil {
			r.Metadata = metadata
		}
	}
	
	return &r, nil
}

// Close shuts down the analytics writer
func (aw *AnalyticsWriter) Close() {
	// Signal shutdown to cleanup goroutine
	close(aw.shutdown)
	
	// Close write queue
	close(aw.writeQueue)
	
	// Wait for writer to finish
	aw.wg.Wait()
	
	// Close database
	if aw.db != nil {
		aw.db.Close()
	}
}

// Analytics HTTP handlers
func (p *Proxy) handleAnalyticsStats(w http.ResponseWriter, r *http.Request) {
	stats := p.analytics.GetStats()
	json.NewEncoder(w).Encode(stats)
}

func (p *Proxy) handleAnalyticsMessages(w http.ResponseWriter, r *http.Request) {
	results, err := p.analytics.Search(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	// Return just the results array for the messages endpoint
	json.NewEncoder(w).Encode(results)
}

func (p *Proxy) handleAnalyticsSearch(w http.ResponseWriter, r *http.Request) {
	results, err := p.analytics.Search(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}

func (p *Proxy) handleAnalyticsModels(w http.ResponseWriter, r *http.Request) {
	models, err := p.analytics.GetModels()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(models)
}

func (p *Proxy) handleAnalyticsDashboard(w http.ResponseWriter, r *http.Request) {
	// Serve the analytics dashboard HTML file
	dashboardPath := filepath.Join(filepath.Dir(os.Args[0]), "analytics_dashboard.html")
	
	// Try same directory as executable first
	if _, err := os.Stat(dashboardPath); os.IsNotExist(err) {
		// Try current working directory
		dashboardPath = "analytics_dashboard.html"
	}
	
	content, err := os.ReadFile(dashboardPath)
	if err != nil {
		// Fallback to simple dashboard if file not found
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Analytics Dashboard</title></head>
<body>
<h1>Analytics Dashboard</h1>
<p>Could not load full dashboard. Please ensure analytics_dashboard.html is in the same directory as the executable.</p>
<p><a href="/analytics/stats">View Stats API</a> | <a href="/analytics/search">Search API</a> | <a href="/metrics">Prometheus Metrics</a></p>
</body>
</html>`))
		return
	}
	
	w.Header().Set("Content-Type", "text/html")
	w.Write(content)
}

func (p *Proxy) handleAnalyticsMessageDetail(w http.ResponseWriter, r *http.Request) {
	// Extract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/analytics/messages/")
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		http.Error(w, "Invalid message ID", http.StatusBadRequest)
		return
	}
	
	message, err := p.analytics.GetMessageByID(id)
	if err != nil {
		http.Error(w, "Message not found", http.StatusNotFound)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(message)
}

func (p *Proxy) handleAnalyticsExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	
	// Check if exporting a single message
	if messageID := r.URL.Query().Get("message_id"); messageID != "" {
		id, err := strconv.ParseInt(messageID, 10, 64)
		if err != nil {
			http.Error(w, "Invalid message ID", http.StatusBadRequest)
			return
		}
		
		message, err := p.analytics.GetMessageByID(id)
		if err != nil {
			http.Error(w, "Message not found", http.StatusNotFound)
			return
		}
		
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=message_%d.%s", id, format))
		if format == "json" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(message)
		}
		return
	}
	
	// Export search results
	results, err := p.analytics.Search(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=analytics_export.%s", format))
	
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Write([]byte("ID,Timestamp,Model,User,Prompt,Response,InputTokens,OutputTokens,Latency,Status\n"))
		for _, r := range results {
			fmt.Fprintf(w, "%d,%s,%s,%s,%q,%q,%d,%d,%.3f,%s\n",
				r.ID, r.Timestamp.Format(time.RFC3339), r.Model, r.User,
				r.Prompt, r.ResponsePreview, r.PromptTokens, r.TokensGenerated,
				r.DurationSeconds, r.Status)
		}
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// truncate limits string length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
