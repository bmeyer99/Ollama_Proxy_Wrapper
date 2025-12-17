package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// serviceLogger is the internal logger instance

// InitServiceLogging sets up file-based logging when running as a Windows service
func InitServiceLogging() error {
	// Always use ProgramData for service mode
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = "C:\\ProgramData"
	}
	logDir := filepath.Join(programData, "OllamaProxy", "logs")
	
	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	
	// Create log file with timestamp
	logFile := filepath.Join(logDir, fmt.Sprintf("ollama-proxy-%s.log", time.Now().Format("2006-01-02")))
	
	// Open log file
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	
	// Create logger and assign to global ServiceLogger
	ServiceLogger = log.New(f, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile)
	
	// Redirect standard log output to file as well
	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	
	ServiceLogger.Printf("=== Service logging initialized ===")
	ServiceLogger.Printf("Log file: %s", logFile)
	ServiceLogger.Printf("Executable: %s", os.Args[0])
	ServiceLogger.Printf("Working directory: %s", getCurrentWorkingDir())
	
	return nil
}

func getCurrentWorkingDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "unknown"
}

// LogError logs errors in service mode
func LogError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if ServiceLogger != nil {
		ServiceLogger.Printf("ERROR: %s", msg)
	} else {
		log.Printf("ERROR: %s", msg)
	}
}

// LogInfo logs info in service mode
func LogInfo(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if ServiceLogger != nil {
		ServiceLogger.Printf("INFO: %s", msg)
	} else {
		log.Printf("INFO: %s", msg)
	}
}