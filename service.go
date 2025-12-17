//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
)

type ollamaProxyService struct {
	elog          debug.Log
	proxy         *Proxy
	ollamaProcess *OllamaProcess
}

func (s *ollamaProxyService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	// Initialize file logging for service mode
	if err := InitServiceLogging(); err != nil {
		s.elog.Error(1, fmt.Sprintf("Failed to initialize logging: %v", err))
		// Continue anyway, but log to event log
	}

	s.elog.Info(1, "Ollama Proxy Service starting")
	LogPrintf("Ollama Proxy Service starting")
	LogPrintf("Working directory: %s", getCurrentDirectory())

	// Find ollama executable
	ollamaPath, err := findOllamaExecutable()
	if err != nil {
		s.elog.Error(1, fmt.Sprintf("Failed to find Ollama: %v", err))
		LogPrintf("ERROR: Failed to find Ollama: %v", err)
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	}

	// Kill any existing Ollama processes on default port
	LogPrintf("Checking for existing Ollama processes...")
	if err := killExistingOllama(); err != nil {
		s.elog.Warning(1, fmt.Sprintf("Failed to kill existing Ollama: %v", err))
		LogPrintf("WARNING: Failed to kill existing Ollama: %v", err)
	}

	// Start Ollama on port 11435 (hidden port)
	s.elog.Info(1, fmt.Sprintf("Starting Ollama from: %s on port 11435", ollamaPath))
	LogPrintf("Starting Ollama from: %s on port 11435", ollamaPath)
	s.ollamaProcess, err = startOllama(ollamaPath, 11435)
	if err != nil {
		s.elog.Error(1, fmt.Sprintf("CRITICAL: Failed to start Ollama: %v", err))
		LogPrintf("CRITICAL ERROR: Failed to start Ollama: %v", err)
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	}
	
	// Cleanup function for Ollama
	defer func() {
		if s.ollamaProcess != nil {
			s.elog.Info(1, "Stopping Ollama process in defer")
			s.ollamaProcess.Stop()
			// Give it time to terminate
			time.Sleep(2 * time.Second)
		}
	}()

	// Wait for Ollama to be ready on 11435
	s.elog.Info(1, "Waiting for Ollama to be ready on port 11435...")
	LogPrintf("Waiting for Ollama to be ready on port 11435...")
	if !waitForOllama("localhost", 11435, 30*time.Second) {
		s.elog.Error(1, "CRITICAL: Ollama did not become ready within 30 seconds")
		LogPrintf("CRITICAL ERROR: Ollama did not become ready within 30 seconds")
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	}
	s.elog.Info(1, "Ollama is ready on port 11435!")
	LogPrintf("Ollama is ready on port 11435!")

	// Start metrics proxy on 11434 (where apps expect Ollama) forwarding to 11435
	LogPrintf("Creating proxy to forward localhost:11434 -> localhost:11435")
	s.proxy = NewProxy("http://localhost:11435", 11434, true)
	
	// Start proxy in background
	go func() {
		LogPrintf("Starting proxy server on port 11434...")
		if err := s.proxy.Start(); err != nil {
			s.elog.Error(1, fmt.Sprintf("Proxy error: %v", err))
			LogPrintf("CRITICAL ERROR: Proxy failed to start: %v", err)
		}
	}()
	
	// Give proxy a moment to start and check if port is listening
	time.Sleep(2 * time.Second)
	
	// Check if proxy is listening on port 11434
	LogPrintf("Checking if proxy is listening on port 11434...")
	if !isPortOpen("localhost", 11434) {
		s.elog.Error(1, "CRITICAL: Proxy failed to bind to port 11434")
		LogPrintf("CRITICAL ERROR: Proxy failed to bind to port 11434")
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	}
	
	s.elog.Info(1, "Proxy started successfully on port 11434")
	LogPrintf("SUCCESS: Proxy is listening on port 11434")

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	s.elog.Info(1, "Ollama Proxy Service started successfully")
	LogPrintf("Ollama Proxy Service is now running")
	LogPrintf("Proxy: http://localhost:11434 -> Ollama: http://localhost:11435")

	// Start health monitoring in background
	stopHealthCheck := make(chan bool)
	go s.monitorOllamaHealth(ollamaPath, stopHealthCheck)
	defer func() {
		stopHealthCheck <- true
	}()

loop:
	for {
		c := <-r
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
			time.Sleep(100 * time.Millisecond)
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			s.elog.Info(1, "Service stop requested")
			// CRITICAL: Stop Ollama process FIRST before shutting down proxy
			if s.ollamaProcess != nil {
				s.elog.Info(1, "Stopping Ollama process...")
				s.ollamaProcess.Stop()
				// Give it time to terminate
				time.Sleep(2 * time.Second)
			}
			// Then shutdown proxy
			if s.proxy != nil {
				s.proxy.Shutdown()
			}
			break loop
		default:
			s.elog.Error(1, fmt.Sprintf("Unexpected control request #%d", c))
		}
	}

	changes <- svc.Status{State: svc.StopPending}
	return false, 0
}

// monitorOllamaHealth monitors Ollama health and restarts if crashed
func (s *ollamaProxyService) monitorOllamaHealth(ollamaPath string, stop <-chan bool) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	consecutiveFailures := 0
	const maxFailures = 1 // Restart immediately on failure

	LogPrintf("Health monitoring started (checking every 30s, 10s timeout)")

	for {
		select {
		case <-stop:
			LogPrintf("Health monitoring stopped")
			return
		case <-ticker.C:
			// Check if Ollama is responsive (10s timeout for faster response)
			if !waitForOllama("localhost", 11435, 10*time.Second) {
				consecutiveFailures++
				s.elog.Warning(1, fmt.Sprintf("Ollama health check failed (%d/%d)", consecutiveFailures, maxFailures))
				LogPrintf("WARNING: Ollama health check failed (%d/%d)", consecutiveFailures, maxFailures)

				if consecutiveFailures >= maxFailures {
					s.elog.Error(1, "Ollama appears to have crashed - attempting restart")
					LogPrintf("CRITICAL: Ollama appears to have crashed - attempting restart")

					// Stop old process
					if s.ollamaProcess != nil {
						s.ollamaProcess.Stop()
						time.Sleep(2 * time.Second)
					}

					// Kill any remaining Ollama processes
					if err := killExistingOllama(); err != nil {
						LogPrintf("Warning: Failed to kill existing Ollama: %v", err)
					}

					// Restart Ollama
					newProcess, err := startOllama(ollamaPath, 11435)
					if err != nil {
						s.elog.Error(1, fmt.Sprintf("Failed to restart Ollama: %v", err))
						LogPrintf("ERROR: Failed to restart Ollama: %v", err)
					} else {
						s.ollamaProcess = newProcess
						if waitForOllama("localhost", 11435, 30*time.Second) {
							s.elog.Info(1, "Ollama restarted successfully")
							LogPrintf("SUCCESS: Ollama restarted successfully")
							consecutiveFailures = 0
						} else {
							s.elog.Error(1, "Ollama restart failed - not responding")
							LogPrintf("ERROR: Ollama restart failed - not responding")
						}
					}
				}
			} else {
				// Health check passed
				if consecutiveFailures > 0 {
					LogPrintf("Ollama health check recovered")
				}
				consecutiveFailures = 0
			}
		}
	}
}

func runAsService() {
	const svcName = "OllamaMetricsProxy"

	isIntSess, err := svc.IsAnInteractiveSession()
	if err != nil {
		log.Fatalf("Failed to determine if we are running in an interactive session: %v", err)
	}

	if isIntSess {
		log.Fatal("Cannot run service in interactive session")
	}

	elog, err := eventlog.Open(svcName)
	if err != nil {
		return
	}
	defer elog.Close()

	err = svc.Run(svcName, &ollamaProxyService{elog: elog})
	if err != nil {
		elog.Error(1, fmt.Sprintf("Service failed: %v", err))
		return
	}
	elog.Info(1, "Service stopped")
}

// IsRunningAsService checks if we're running as a Windows service
func IsRunningAsService() bool {
	// Check if running from System32 (typical for services)
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		if strings.Contains(strings.ToLower(exeDir), "system32") {
			return true
		}
	}
	
	// Also check if we can detect interactive session
	if isIntSess, err := svc.IsAnInteractiveSession(); err == nil {
		return !isIntSess
	}
	
	return false
}