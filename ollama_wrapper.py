#!/usr/bin/env python3
"""
Transparent Ollama Wrapper with Metrics
Usage: python ollama_wrapper.py [ollama commands]
Examples:
    python ollama_wrapper.py list
    python ollama_wrapper.py run phi4
    python ollama_wrapper.py pull llama3.2
"""

import sys
import subprocess
import time
import os
import signal
import threading
import requests
from pathlib import Path

# Import the hybrid proxy (make sure it's in the same directory)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
try:
    from ollama_hybrid_proxy import HybridOllamaProxy
except ImportError:
    print("Error: ollama_hybrid_proxy.py must be in the same directory as this script")
    print(f"Current directory: {os.path.dirname(os.path.abspath(__file__))}")
    sys.exit(1)

# Configuration
OLLAMA_REAL_PORT = 11435  # Where Ollama will actually run
PROXY_PORT = 11434        # Where apps expect Ollama (default port)
STARTUP_TIMEOUT = 30      # Seconds to wait for Ollama to start

# Commands that should just run and exit
PASSTHROUGH_COMMANDS = ['list', 'ps', 'rm', 'cp', 'help', '--version', '-v']

def is_port_open(port, host='localhost'):
    """Check if a port is open"""
    import socket
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(1)
    result = sock.connect_ex((host, port))
    sock.close()
    return result == 0

def wait_for_ollama(port, timeout=STARTUP_TIMEOUT):
    """Wait for Ollama to be ready"""
    print(f"Waiting for Ollama to start on port {port}...")
    start_time = time.time()
    
    while time.time() - start_time < timeout:
        # First check if port is open
        if is_port_open(port):
            print(f"✓ Port {port} is open, testing API...")
            try:
                response = requests.get(f'http://localhost:{port}/api/tags', timeout=2)
                if response.status_code == 200:
                    print(f"✓ Ollama is ready on port {port}")
                    return True
                else:
                    print(f"Port {port} open but API returned {response.status_code}")
            except Exception as e:
                print(f"Port {port} open but API failed: {e}")
        else:
            print(f"Waiting for port {port} to open...")
        time.sleep(1)
    
    print(f"✗ Timeout waiting for Ollama on port {port}")
    return False

def run_passthrough_command(args):
    """Run a command that doesn't need the proxy"""
    cmd = ['ollama'] + args
    result = subprocess.run(cmd, capture_output=False)
    return result.returncode

def get_ollama_executable():
    """Find the ollama executable"""
    # First try the PATH
    ollama_path = subprocess.run(['where', 'ollama'], capture_output=True, text=True).stdout.strip()
    if ollama_path and os.path.exists(ollama_path):
        return ollama_path
    
    # Common Windows locations
    common_paths = [
        r"C:\Users\%USERNAME%\AppData\Local\Programs\Ollama\ollama.exe",
        r"C:\Program Files\Ollama\ollama.exe",
        r"C:\ollama\ollama.exe"
    ]
    
    for path in common_paths:
        expanded_path = os.path.expandvars(path)
        if os.path.exists(expanded_path):
            return expanded_path
    
    # Just return 'ollama' and hope it's in PATH
    return 'ollama'

def main():
    # Parse command line arguments
    if len(sys.argv) < 2:
        print("Usage: python ollama_wrapper.py [ollama commands]")
        print("Examples:")
        print("  python ollama_wrapper.py list")
        print("  python ollama_wrapper.py run phi4")
        print("  python ollama_wrapper.py serve  # Start with metrics proxy")
        sys.exit(1)
    
    ollama_args = sys.argv[1:]
    command = ollama_args[0] if ollama_args else 'serve'
    
    # Check if this is a simple passthrough command
    if command in PASSTHROUGH_COMMANDS or (len(ollama_args) == 1 and command in ['list', 'ps']):
        print(f"Running: ollama {' '.join(ollama_args)}")
        sys.exit(run_passthrough_command(ollama_args))
    
    # Check if ports are already in use
    if is_port_open(PROXY_PORT):
        print(f"✗ Error: Port {PROXY_PORT} is already in use (existing Ollama or proxy?)")
        print(f"  Stop the existing process or use a different port")
        sys.exit(1)
    
    if is_port_open(OLLAMA_REAL_PORT):
        print(f"✗ Error: Port {OLLAMA_REAL_PORT} is already in use")
        sys.exit(1)
    
    print("=" * 60)
    print("  Ollama Transparent Metrics Wrapper")
    print("=" * 60)
    print(f"Starting Ollama on port {OLLAMA_REAL_PORT} (hidden)")
    print(f"Starting proxy on port {PROXY_PORT} (your apps connect here)")
    print("=" * 60)
    
    # Find ollama executable
    ollama_exe = get_ollama_executable()
    
    # Prepare Ollama command with custom port
    env = os.environ.copy()
    env['OLLAMA_HOST'] = f'0.0.0.0:{OLLAMA_REAL_PORT}'
    print(f"Setting OLLAMA_HOST environment variable to: {env['OLLAMA_HOST']}")
    
    # Determine if we need to start serve
    if command not in ['serve', 'start']:
        # User wants to run a model, so we need to start serve first
        serve_cmd = [ollama_exe, 'serve']
        print(f"Starting Ollama server: {' '.join(serve_cmd)}")
        
        # Start Ollama serve in background
        print(f"Setting OLLAMA_HOST = {env.get('OLLAMA_HOST', 'not set')}")
        
        ollama_process = subprocess.Popen(
            serve_cmd,
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL
        )
        
        # Wait for Ollama to be ready
        if not wait_for_ollama(OLLAMA_REAL_PORT):
            print("✗ Error: Ollama failed to start")
            ollama_process.terminate()
            sys.exit(1)
        
        # Now run the actual command (e.g., "run phi4")
        run_cmd = [ollama_exe] + ollama_args
        print(f"\nRunning: {' '.join(run_cmd)}")
        
        # Create a new environment for the run command to connect through the proxy
        run_env = os.environ.copy()
        run_env['OLLAMA_HOST'] = f'http://localhost:{PROXY_PORT}'
        print(f"Setting run command OLLAMA_HOST to proxy: {run_env['OLLAMA_HOST']}")
        
        # Start the proxy in a thread
        proxy_thread = threading.Thread(target=start_proxy, daemon=True)
        proxy_thread.start()
        
        # Give proxy a moment to start
        time.sleep(2)
        
        print("\n" + "=" * 60)
        print("✓ Metrics proxy is running!")
        print(f"✓ Your apps can connect to: http://localhost:{PROXY_PORT}")
        print(f"✓ View metrics at: http://localhost:{PROXY_PORT}/metrics")
        print(f"✓ View analytics at: http://localhost:{PROXY_PORT}/analytics/stats")
        print("=" * 60)
        print("✓ Running your Ollama command...")
        print("  (The proxy continues running in the background)")
        print("=" * 60 + "\n")
        
        # Run the user's command interactively
        run_process = subprocess.Popen(run_cmd, env=run_env)
        
        try:
            # Wait for the run command to complete
            run_process.wait()
        except KeyboardInterrupt:
            print("\nShutting down...")
            run_process.terminate()
            ollama_process.terminate()
            sys.exit(0)
        
    else:
        # Just starting serve
        serve_cmd = [ollama_exe, 'serve']
        print(f"Starting: {' '.join(serve_cmd)}")
        
        # Start Ollama serve
        print(f"Setting OLLAMA_HOST = {env.get('OLLAMA_HOST', 'not set')}")
        
        ollama_process = subprocess.Popen(
            serve_cmd,
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL
        )
        
        # Wait for Ollama to be ready
        if not wait_for_ollama(OLLAMA_REAL_PORT):
            print("✗ Error: Ollama failed to start")
            ollama_process.terminate()
            sys.exit(1)
        
        # Start the proxy
        start_proxy()
    
    # Keep running until interrupted
    try:
        while True:
            time.sleep(1)
            # Check if Ollama is still running
            if ollama_process.poll() is not None:
                print("\nOllama process ended")
                break
    except KeyboardInterrupt:
        print("\nShutting down...")
    
    # Cleanup
    if 'ollama_process' in locals():
        ollama_process.terminate()
        ollama_process.wait()
    
    print("Goodbye!")

def start_proxy():
    """Start the metrics proxy"""
    print(f"Starting metrics proxy on port {PROXY_PORT}...")
    
    # Create and run the proxy
    proxy = HybridOllamaProxy(
        ollama_host=f'http://localhost:{OLLAMA_REAL_PORT}',
        proxy_port=PROXY_PORT,
        analytics_backend='sqlite'  # or 'jsonl'
    )
    
    try:
        proxy.run()
    except Exception as e:
        print(f"Proxy error: {e}")

if __name__ == '__main__':
    main()