package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	OllamaRealPort = 11435 // Where Ollama will actually run
	ProxyPort      = 11434 // Where apps expect Ollama (default port)
	StartupTimeout = 30 * time.Second
)

var (
	// Commands that should start the proxy server
	proxyCommands = []string{"serve"}
	
	// All other commands are passed through to Ollama
)

func main() {
	// Check if running as Windows service first
	serviceFlag := flag.Bool("service", false, "Run as Windows service")
	flag.Parse()
	
	if *serviceFlag {
		runAsService()
		return
	}

	// After flag.Parse(), remaining args are in flag.Args()
	remainingArgs := flag.Args()
	
	// If no command provided, default to "serve"
	command := "serve"
	args := []string{}
	
	if len(remainingArgs) > 0 {
		command = remainingArgs[0]
		if len(remainingArgs) > 1 {
			args = remainingArgs[1:]
		}
	}

	// Check if this is a proxy command (serve), otherwise passthrough
	if !isProxyCommand(command) {
		exitCode := runPassthroughCommand(command, args)
		os.Exit(exitCode)
	}

	// Check if ports are available (single unified check)
	if isPortOpen("localhost", ProxyPort) {
		log.Fatalf("Error: Port %d is already in use (existing Ollama or proxy?)\nStop the existing process or use a different port", ProxyPort)
	}

	if isPortOpen("localhost", OllamaRealPort) {
		log.Fatalf("Error: Port %d is already in use", OllamaRealPort)
	}

	printBanner()

	// Find ollama executable
	ollamaPath, err := findOllamaExecutable()
	if err != nil {
		log.Fatalf("Error finding Ollama: %v", err)
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Kill any existing Ollama processes
	if err := killExistingOllama(); err != nil {
		log.Printf("Warning: Failed to kill existing Ollama: %v", err)
		// Continue anyway, it might work
	}
	
	// Start Ollama process
	ollamaProcess, err := startOllama(ollamaPath, OllamaRealPort)
	if err != nil {
		log.Fatalf("Failed to start Ollama: %v", err)
	}
	defer func() {
		if ollamaProcess != nil {
			ollamaProcess.Stop()
		}
	}()

	// Wait for Ollama to be ready
	if !waitForOllama("localhost", OllamaRealPort, StartupTimeout) {
		log.Fatal("Ollama failed to start")
	}

	// Start metrics proxy
	proxy := NewProxy(fmt.Sprintf("http://localhost:%d", OllamaRealPort), ProxyPort)
	defer proxy.Shutdown()
	
	go func() {
		if err := proxy.Start(); err != nil {
			log.Printf("Proxy error: %v", err)
		}
	}()

	// Give proxy a moment to start
	time.Sleep(2 * time.Second)

	printProxyReady()

	// Handle specific commands if not "serve"
	if command != "serve" && command != "start" {
		// Run the actual command (e.g., "run phi4")
		runOllamaCommand(ollamaPath, command, args, ProxyPort)
	}

	// Wait for interrupt signal
	<-sigChan
	fmt.Println("\nShutting down...")
}

func printUsage() {
	fmt.Println("Usage: ollama-proxy [ollama commands]")
	fmt.Println("Examples:")
	fmt.Println("  ollama-proxy list")
	fmt.Println("  ollama-proxy run phi4")
	fmt.Println("  ollama-proxy serve  # Start with metrics proxy")
}

func printBanner() {
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("  Ollama Transparent Metrics Wrapper (Go Edition)")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Starting Ollama on port %d (hidden)\n", OllamaRealPort)
	fmt.Printf("Starting proxy on port %d (your apps connect here)\n", ProxyPort)
	fmt.Println(strings.Repeat("=", 60))
}

func printProxyReady() {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("✓ Metrics proxy is running!")
	fmt.Printf("✓ Your apps can connect to: http://localhost:%d\n", ProxyPort)
	fmt.Printf("✓ View metrics at: http://localhost:%d/metrics\n", ProxyPort)
	fmt.Printf("✓ View analytics at: http://localhost:%d/analytics/stats\n", ProxyPort)
	fmt.Println(strings.Repeat("=", 60))
}

func isProxyCommand(command string) bool {
	for _, cmd := range proxyCommands {
		if command == cmd {
			return true
		}
	}
	return false
}

