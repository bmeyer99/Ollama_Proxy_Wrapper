package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// OllamaProcess manages the Ollama subprocess
type OllamaProcess struct {
	cmd  *exec.Cmd
	port int
}

// Stop terminates the Ollama process
func (op *OllamaProcess) Stop() {
	if op == nil || op.cmd == nil || op.cmd.Process == nil {
		return
	}
	
	log.Printf("Stopping Ollama process (PID: %d)", op.cmd.Process.Pid)
	
	// On Windows, we need to kill the process tree
	if runtime.GOOS == "windows" {
		// Use taskkill to kill the process and all its children
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", op.cmd.Process.Pid))
		if output, err := killCmd.CombinedOutput(); err != nil {
			log.Printf("Failed to kill process tree with taskkill: %v - %s", err, output)
			// Fallback to regular kill
			if err := op.cmd.Process.Kill(); err != nil {
				log.Printf("Failed to kill process: %v", err)
			}
		} else {
			log.Printf("Successfully killed Ollama process tree")
		}
	} else {
		// On Unix-like systems, use regular kill
		if err := op.cmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill process: %v", err)
		}
	}
	
	// Wait for process to exit
	op.cmd.Wait()
}

// findOllamaExecutable locates the ollama executable
func findOllamaExecutable() (string, error) {
	// First check if set via service environment variable
	if envPath := os.Getenv("OLLAMA_EXECUTABLE_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			log.Printf("Found Ollama via service environment: %s", envPath)
			return envPath, nil
		}
		log.Printf("Service environment path invalid: %s", envPath)
	}
	
	// Then try the PATH
	if path, err := exec.LookPath("ollama"); err == nil {
		log.Printf("Found Ollama in PATH: %s", path)
		return path, nil
	}

	// Common locations based on OS
	var commonPaths []string
	if runtime.GOOS == "windows" {
		// Get current user's home directory (even when running as service)
		userProfile := os.Getenv("USERPROFILE")
		if userProfile == "" {
			userProfile = os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
		}
		
		// Also check all user profiles for Ollama installations
		commonPaths = []string{
			// System-wide installations
			`C:\Program Files\Ollama\ollama.exe`,
			`C:\Program Files (x86)\Ollama\ollama.exe`,
			`C:\ollama\ollama.exe`,
			
			// Current user installation
			filepath.Join(userProfile, "AppData", "Local", "Programs", "Ollama", "ollama.exe"),
			
			// Check other common user profile locations
			`C:\Users\Administrator\AppData\Local\Programs\Ollama\ollama.exe`,
		}
		
		// Add all user directories
		if userDirs, err := os.ReadDir(`C:\Users`); err == nil {
			for _, userDir := range userDirs {
				if userDir.IsDir() {
					userPath := filepath.Join(`C:\Users`, userDir.Name(), "AppData", "Local", "Programs", "Ollama", "ollama.exe")
					commonPaths = append(commonPaths, userPath)
				}
			}
		}
	} else if runtime.GOOS == "darwin" {
		commonPaths = []string{
			"/usr/local/bin/ollama",
			"/opt/homebrew/bin/ollama",
			"/Applications/Ollama.app/Contents/Resources/ollama",
		}
	} else { // Linux
		commonPaths = []string{
			"/usr/local/bin/ollama",
			"/usr/bin/ollama",
			"/opt/ollama/ollama",
		}
	}

	log.Printf("Searching for Ollama in %d locations...", len(commonPaths))
	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			log.Printf("Found Ollama at: %s", path)
			return path, nil
		}
	}

	// Last resort: try to find it using where command on Windows
	if runtime.GOOS == "windows" {
		cmd := exec.Command("where", "ollama")
		if output, err := cmd.Output(); err == nil {
			path := strings.TrimSpace(string(output))
			if path != "" {
				lines := strings.Split(path, "\n")
				if len(lines) > 0 {
					foundPath := strings.TrimSpace(lines[0])
					log.Printf("Found Ollama using 'where' command: %s", foundPath)
					return foundPath, nil
				}
			}
		}
	}

	return "", fmt.Errorf("ollama executable not found in PATH or common locations:\n%v", commonPaths)
}

// killExistingOllama kills any existing Ollama processes
func killExistingOllama() error {
	log.Println("Checking for existing Ollama processes...")
	
	if runtime.GOOS == "windows" {
		// On Windows, use taskkill
		cmd := exec.Command("taskkill", "/F", "/IM", "ollama.exe")
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Check if the error is because no process was found
			if strings.Contains(string(output), "not found") || strings.Contains(string(output), "ERROR") {
				log.Println("No existing Ollama process found")
				return nil
			}
			return fmt.Errorf("failed to kill Ollama: %w - %s", err, output)
		}
		log.Println("Killed existing Ollama process")
	} else {
		// On Unix-like systems, use pkill
		cmd := exec.Command("pkill", "-f", "ollama.*serve")
		output, err := cmd.CombinedOutput()
		if err != nil {
			// pkill returns 1 if no processes were found, which is fine
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				log.Println("No existing Ollama process found")
				return nil
			}
			return fmt.Errorf("failed to kill Ollama: %w - %s", err, output)
		}
		log.Println("Killed existing Ollama process")
	}
	
	// Wait a moment for the process to fully terminate
	time.Sleep(2 * time.Second)
	return nil
}

// checkPort checks if a port is already in use
func checkPort(port int) error {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use", port)
	}
	ln.Close()
	return nil
}

// startOllama starts the Ollama process on the specified port
func startOllama(ollamaPath string, port int) (*OllamaProcess, error) {
	env := append(os.Environ(), 
		fmt.Sprintf("OLLAMA_HOST=0.0.0.0:%d", port),
		"OLLAMA_KEEP_ALIVE=-1",  // Keep models loaded for 5 minutes
	)
	
	log.Printf("Starting Ollama server on port %d", port)
	cmd := exec.Command(ollamaPath, "serve")
	cmd.Env = env
	
	// Configure Windows-specific process attributes
	configureCommand(cmd)
	
	// Capture output when running as service
	if IsRunningAsService() && ServiceLogger != nil {
		// Create pipes for stdout and stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
		}
		
		// Start goroutines to read output
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				ServiceLogger.Printf("[Ollama stdout] %s", scanner.Text())
			}
			if err := scanner.Err(); err != nil && err != io.EOF {
				ServiceLogger.Printf("[Ollama stdout] Read error: %v", err)
			}
		}()
		
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				ServiceLogger.Printf("[Ollama stderr] %s", scanner.Text())
			}
			if err := scanner.Err(); err != nil && err != io.EOF {
				ServiceLogger.Printf("[Ollama stderr] Read error: %v", err)
			}
		}()
	}
	
	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Ollama: %w", err)
	}

	return &OllamaProcess{cmd: cmd, port: port}, nil
}

// isPortOpen checks if a port is open
func isPortOpen(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// waitForOllama waits for Ollama to be ready
func waitForOllama(host string, port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	
	fmt.Printf("Waiting for Ollama to start on port %d...\n", port)
	
	for time.Now().Before(deadline) {
		if isPortOpen(host, port) {
			fmt.Printf("[OK] Port %d is open, testing API...\n", port)
			
			// Test the API endpoint
			resp, err := http.Get(fmt.Sprintf("http://%s:%d/api/tags", host, port))
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				fmt.Printf("[OK] Ollama is ready on port %d\n", port)
				return true
			}
			if err == nil {
				resp.Body.Close()
				fmt.Printf("Port %d open but API returned %d\n", port, resp.StatusCode)
			} else {
				fmt.Printf("Port %d open but API failed: %v\n", port, err)
			}
		} else {
			fmt.Printf("Waiting for port %d to open...\n", port)
		}
		time.Sleep(1 * time.Second)
	}
	
	fmt.Printf("[ERROR] Timeout waiting for Ollama on port %d\n", port)
	return false
}

// runPassthroughCommand runs a command that doesn't need the proxy
func runPassthroughCommand(command string, args []string) int {
	cmdArgs := append([]string{command}, args...)
	cmd := exec.Command("ollama", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode()
		}
		return 1
	}
	return 0
}

// runOllamaCommand runs an interactive Ollama command through the proxy
func runOllamaCommand(ollamaPath string, command string, args []string, proxyPort int) {
	env := append(os.Environ(), fmt.Sprintf("OLLAMA_HOST=http://localhost:%d", proxyPort))
	
	cmdArgs := append([]string{command}, args...)
	fmt.Printf("\nRunning: ollama %s\n", strings.Join(cmdArgs, " "))
	
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("✓ Running your Ollama command...")
	fmt.Println("  (The proxy continues running in the background)")
	fmt.Println(strings.Repeat("=", 60) + "\n")
	
	cmd := exec.Command(ollamaPath, cmdArgs...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	
	// Run the command
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		log.Printf("Command failed: %v", err)
		os.Exit(1)
	}
}