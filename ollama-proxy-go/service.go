//go:build windows

package main

import (
	"fmt"
	"log"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
)

type ollamaProxyService struct {
	elog  debug.Log
	proxy *Proxy
}

func (s *ollamaProxyService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	s.elog.Info(1, "Ollama Proxy Service starting")

	// Find ollama executable
	ollamaPath, err := findOllamaExecutable()
	if err != nil {
		s.elog.Error(1, fmt.Sprintf("Failed to find Ollama: %v", err))
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	}

	// Kill any existing Ollama processes on default port
	if err := killExistingOllama(); err != nil {
		s.elog.Warning(1, fmt.Sprintf("Failed to kill existing Ollama: %v", err))
	}

	// Start Ollama on port 11435 (hidden port)
	s.elog.Info(1, fmt.Sprintf("Starting Ollama from: %s on port 11435", ollamaPath))
	ollamaProcess, err := startOllama(ollamaPath, 11435)
	if err != nil {
		s.elog.Error(1, fmt.Sprintf("CRITICAL: Failed to start Ollama: %v", err))
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	}
	
	// Cleanup function for Ollama
	defer func() {
		if ollamaProcess != nil {
			s.elog.Info(1, "Stopping Ollama process")
			ollamaProcess.Stop()
		}
	}()

	// Wait for Ollama to be ready on 11435
	s.elog.Info(1, "Waiting for Ollama to be ready on port 11435...")
	if !waitForOllama("localhost", 11435, 30*time.Second) {
		s.elog.Error(1, "CRITICAL: Ollama did not become ready within 30 seconds")
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	}
	s.elog.Info(1, "Ollama is ready on port 11435!")

	// Start metrics proxy on 11434 (where apps expect Ollama) forwarding to 11435
	s.proxy = NewProxy("http://localhost:11435", 11434)
	
	// Start proxy in background
	go func() {
		if err := s.proxy.Start(); err != nil {
			s.elog.Error(1, fmt.Sprintf("Proxy error: %v", err))
		}
	}()
	
	// Give proxy a moment to start and check if port is listening
	time.Sleep(2 * time.Second)
	
	// Check if proxy is listening on port 11434
	if !isPortOpen("localhost", 11434) {
		s.elog.Error(1, "CRITICAL: Proxy failed to bind to port 11434")
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	}
	
	s.elog.Info(1, "Proxy started successfully on port 11434")

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	s.elog.Info(1, "Ollama Proxy Service started successfully")

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
			// Ensure proper cleanup
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