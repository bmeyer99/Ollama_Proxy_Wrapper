#!/usr/bin/env python3
"""
Ollama Metrics Proxy Runner
Simple runner for Windows service via WinSW - no service framework complexity
"""

import sys
import os
import time
import signal
import logging
import subprocess
from pathlib import Path

# Add current directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from ollama_wrapper import start_proxy, get_ollama_executable, wait_for_ollama, is_port_open

# Configuration
OLLAMA_REAL_PORT = 11435
PROXY_PORT = 11434
SHUTDOWN_TIMEOUT = 30

# Setup logging
log_dir = Path(os.getenv('TEMP', 'C:\\temp')) / 'ollama_service'
log_dir.mkdir(exist_ok=True)
log_file = log_dir / 'ollama_runner.log'

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler(log_file),
        logging.StreamHandler()
    ]
)

logger = logging.getLogger(__name__)

class OllamaRunner:
    def __init__(self):
        self.running = True
        self.ollama_process = None
        self.proxy_process = None
        
        # Set up signal handlers for graceful shutdown
        signal.signal(signal.SIGTERM, self._signal_handler)
        signal.signal(signal.SIGINT, self._signal_handler)
    
    def _signal_handler(self, signum, frame):
        """Handle shutdown signals"""
        logger.info(f"Received signal {signum}, shutting down...")
        self.running = False
    
    def start_ollama(self):
        """Start Ollama on the internal port"""
        logger.info("Starting Ollama server...")
        
        # Try environment variable first (for service mode)
        ollama_exe = os.getenv('OLLAMA_EXECUTABLE')
        logger.info(f"Environment OLLAMA_EXECUTABLE: {ollama_exe}")
        
        if not ollama_exe or not os.path.exists(ollama_exe):
            logger.info("Environment variable not set or file doesn't exist, trying detection...")
            # Fallback to detection
            ollama_exe = get_ollama_executable()
            logger.info(f"Detected Ollama path: {ollama_exe}")
            
        if not ollama_exe:
            raise RuntimeError("Ollama executable not found")
        
        logger.info(f"Using Ollama executable: {ollama_exe}")
        
        # Set environment to bind Ollama to internal port
        env = os.environ.copy()
        env['OLLAMA_HOST'] = f'0.0.0.0:{OLLAMA_REAL_PORT}'
        
        try:
            self.ollama_process = subprocess.Popen(
                [ollama_exe, 'serve'],
                env=env,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                creationflags=subprocess.CREATE_NO_WINDOW
            )
            logger.info(f"Ollama started with PID {self.ollama_process.pid}")
            
            # Wait for Ollama to be ready
            if not wait_for_ollama(port=OLLAMA_REAL_PORT, timeout=30):
                raise RuntimeError("Ollama failed to start within timeout")
            
            logger.info(f"Ollama is ready on port {OLLAMA_REAL_PORT}")
            return True
            
        except Exception as e:
            logger.error(f"Failed to start Ollama: {e}")
            return False
    
    def start_proxy(self):
        """Start the metrics proxy"""
        logger.info("Starting metrics proxy...")
        
        try:
            # Import and start proxy in a separate process
            proxy_script = os.path.join(os.path.dirname(__file__), 'ollama_fastapi_proxy.py')
            
            self.proxy_process = subprocess.Popen(
                [sys.executable, proxy_script, 
                 '--ollama-host', f'http://localhost:{OLLAMA_REAL_PORT}',
                 '--proxy-port', str(PROXY_PORT),
                 '--analytics-backend', 'sqlite'],
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                creationflags=subprocess.CREATE_NO_WINDOW
            )
            logger.info(f"Proxy started with PID {self.proxy_process.pid}")
            
            # Wait for proxy to be ready
            time.sleep(3)
            if not is_port_open(PROXY_PORT, 'localhost'):
                # Get proxy process output for debugging
                if self.proxy_process.poll() is not None:
                    stdout, stderr = self.proxy_process.communicate()
                    logger.error(f"Proxy process exited with code {self.proxy_process.returncode}")
                    logger.error(f"Proxy stdout: {stdout.decode() if stdout else 'None'}")
                    logger.error(f"Proxy stderr: {stderr.decode() if stderr else 'None'}")
                raise RuntimeError("Proxy failed to bind to port")
            
            logger.info(f"Proxy is ready on port {PROXY_PORT}")
            return True
            
        except Exception as e:
            logger.error(f"Failed to start proxy: {e}")
            return False
    
    def stop_services(self):
        """Stop all services gracefully"""
        logger.info("Stopping services...")
        
        # Stop proxy
        if self.proxy_process and self.proxy_process.poll() is None:
            logger.info("Stopping proxy...")
            self.proxy_process.terminate()
            try:
                self.proxy_process.wait(timeout=10)
                logger.info("Proxy stopped")
            except subprocess.TimeoutExpired:
                logger.warning("Force killing proxy...")
                self.proxy_process.kill()
        
        # Stop Ollama
        if self.ollama_process and self.ollama_process.poll() is None:
            logger.info("Stopping Ollama...")
            self.ollama_process.terminate()
            try:
                self.ollama_process.wait(timeout=10)
                logger.info("Ollama stopped")
            except subprocess.TimeoutExpired:
                logger.warning("Force killing Ollama...")
                self.ollama_process.kill()
    
    def run(self):
        """Main run loop"""
        logger.info("Starting Ollama Metrics Proxy Runner")
        logger.info(f"Log file: {log_file}")
        
        try:
            # Start services
            if not self.start_ollama():
                logger.error("Failed to start Ollama")
                return 1
            
            if not self.start_proxy():
                logger.error("Failed to start proxy")
                return 1
            
            logger.info("All services started successfully")
            logger.info(f"Proxy available at: http://localhost:{PROXY_PORT}")
            logger.info(f"Metrics available at: http://localhost:{PROXY_PORT}/metrics")
            logger.info(f"Analytics available at: http://localhost:{PROXY_PORT}/analytics/stats")
            
            # Main loop - just keep running and monitor processes
            while self.running:
                time.sleep(5)
                
                # Check if processes are still running
                if self.ollama_process and self.ollama_process.poll() is not None:
                    logger.error("Ollama process died unexpectedly")
                    self.running = False
                    break
                
                if self.proxy_process and self.proxy_process.poll() is not None:
                    logger.error("Proxy process died unexpectedly")
                    self.running = False
                    break
            
            logger.info("Main loop exited")
            return 0
            
        except Exception as e:
            logger.error(f"Fatal error: {e}")
            return 1
        
        finally:
            self.stop_services()
            logger.info("Runner shutdown complete")


def main():
    """Main entry point"""
    runner = OllamaRunner()
    exit_code = runner.run()
    sys.exit(exit_code)


if __name__ == '__main__':
    main()