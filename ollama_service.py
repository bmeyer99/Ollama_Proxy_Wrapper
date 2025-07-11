#!/usr/bin/env python3
"""
Ollama Metrics Proxy Windows Service
Runs the Ollama proxy wrapper as a Windows service with system tray icon
"""

import sys
import os
import time
import threading
import logging
import subprocess
from pathlib import Path

# Service-related imports (only available on Windows)
try:
    import win32serviceutil
    import win32service
    import win32event
    import servicemanager
    WINDOWS_SERVICE_AVAILABLE = True
except ImportError:
    WINDOWS_SERVICE_AVAILABLE = False
    print("Warning: Windows service modules not available. Install with: pip install pywin32")

# System tray imports
try:
    import tkinter as tk
    from tkinter import messagebox
    import threading
    TRAY_AVAILABLE = True
except ImportError:
    TRAY_AVAILABLE = False
    print("Warning: tkinter not available for system tray")

# Import our proxy wrapper
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ollama_wrapper import start_proxy, get_ollama_executable, wait_for_ollama, is_port_open

# Configuration
OLLAMA_REAL_PORT = 11435
PROXY_PORT = 11434
SERVICE_NAME = "OllamaMetricsProxy"
SERVICE_DISPLAY_NAME = "Ollama Metrics Proxy Service"
SERVICE_DESCRIPTION = "Transparent metrics proxy for Ollama with Prometheus monitoring"

# Setup logging
log_dir = Path(os.getenv('TEMP', 'C:\\temp')) / 'ollama_service'
log_dir.mkdir(exist_ok=True)
log_file = log_dir / 'ollama_service.log'

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler(log_file),
        logging.StreamHandler()
    ]
)
logger = logging.getLogger(__name__)


class SystemTrayIcon:
    """Simple system tray icon using tkinter"""
    
    def __init__(self, service_instance=None):
        self.service = service_instance
        self.root = None
        self.running = False
        
    def create_icon(self):
        """Create a simple text-based icon representing 'Ollama behind bars'"""
        # Using Unicode characters for a simple icon
        return "🦙🔒"  # Llama + lock, or we could use bars: "🦙|"
        
    def start_tray(self):
        """Start the system tray icon"""
        if not TRAY_AVAILABLE:
            logger.warning("System tray not available - tkinter not installed")
            return
            
        try:
            self.running = True
            self.root = tk.Tk()
            self.root.withdraw()  # Hide the main window
            
            # Create a simple status window that can be shown/hidden
            self.create_status_window()
            
            # Start the GUI loop
            while self.running:
                try:
                    self.root.update()
                    time.sleep(0.1)
                except tk.TclError:
                    break
                    
        except Exception as e:
            logger.error(f"System tray error: {e}")
            
    def create_status_window(self):
        """Create a status window that can be shown"""
        self.status_window = tk.Toplevel(self.root)
        self.status_window.title("Ollama Metrics Proxy")
        self.status_window.geometry("400x300")
        self.status_window.withdraw()  # Start hidden
        
        # Icon label
        icon_label = tk.Label(self.status_window, text=self.create_icon(), font=("Arial", 24))
        icon_label.pack(pady=10)
        
        # Status label
        self.status_label = tk.Label(self.status_window, text="Service Status: Running", font=("Arial", 12))
        self.status_label.pack(pady=5)
        
        # Information
        info_text = f"""
Ollama Metrics Proxy Service

Proxy Port: {PROXY_PORT}
Ollama Port: {OLLAMA_REAL_PORT}

Endpoints:
• http://localhost:{PROXY_PORT}/metrics (Prometheus)
• http://localhost:{PROXY_PORT}/analytics/stats

Log File: {log_file}
"""
        info_label = tk.Label(self.status_window, text=info_text, justify=tk.LEFT, font=("Courier", 9))
        info_label.pack(pady=10, padx=20)
        
        # Buttons
        button_frame = tk.Frame(self.status_window)
        button_frame.pack(pady=10)
        
        tk.Button(button_frame, text="Hide", command=self.status_window.withdraw).pack(side=tk.LEFT, padx=5)
        tk.Button(button_frame, text="Stop Service", command=self.stop_service).pack(side=tk.LEFT, padx=5)
        
        # System tray simulation - right-click on title bar or use a button
        tk.Button(self.status_window, text="📊 Show Status", command=self.show_status).pack(pady=5)
        
        # Handle window close
        self.status_window.protocol("WM_DELETE_WINDOW", self.status_window.withdraw)
        
    def show_status(self):
        """Show the status window"""
        if hasattr(self, 'status_window'):
            self.status_window.deiconify()
            self.status_window.lift()
            
    def stop_service(self):
        """Stop the service"""
        if self.service:
            try:
                self.service.stop_service()
            except Exception as e:
                logger.error(f"Error stopping service: {e}")
        self.running = False
        if self.root:
            self.root.quit()
            
    def stop_tray(self):
        """Stop the system tray"""
        self.running = False
        if self.root:
            self.root.quit()


class OllamaProxyService(win32serviceutil.ServiceFramework if WINDOWS_SERVICE_AVAILABLE else object):
    """Windows service for Ollama Metrics Proxy"""
    
    if WINDOWS_SERVICE_AVAILABLE:
        _svc_name_ = SERVICE_NAME
        _svc_display_name_ = SERVICE_DISPLAY_NAME
        _svc_description_ = SERVICE_DESCRIPTION
        _svc_deps_ = None
        _exe_name_ = sys.executable
        _exe_args_ = f'"{__file__}"'
    
    def __init__(self, args=None):
        if WINDOWS_SERVICE_AVAILABLE:
            win32serviceutil.ServiceFramework.__init__(self, args)
            self.hWaitStop = win32event.CreateEvent(None, 0, 0, None)
        
        self.running = False
        self.ollama_process = None
        self.proxy_thread = None
        self.tray_thread = None
        self.tray_icon = None
        
    def SvcStop(self):
        """Stop the service"""
        if WINDOWS_SERVICE_AVAILABLE:
            self.ReportServiceStatus(win32service.SERVICE_STOP_PENDING)
            win32event.SetEvent(self.hWaitStop)
        self.stop_service()
        
    def SvcDoRun(self):
        """Run the service"""
        logger.info("Starting Ollama Metrics Proxy Service")
        if WINDOWS_SERVICE_AVAILABLE:
            servicemanager.LogMsg(
                servicemanager.EVENTLOG_INFORMATION_TYPE,
                servicemanager.PYS_SERVICE_STARTED,
                (self._svc_name_, '')
            )
        
        self.start_service()
        
        if WINDOWS_SERVICE_AVAILABLE:
            # Wait for stop signal
            win32event.WaitForSingleObject(self.hWaitStop, win32event.INFINITE)
        else:
            # Keep running for console mode
            try:
                while self.running:
                    time.sleep(1)
            except KeyboardInterrupt:
                logger.info("Received interrupt signal")
                
        self.stop_service()
        
    def start_service(self):
        """Start the Ollama proxy service"""
        try:
            self.running = True
            
            # Check if ports are available
            if is_port_open(PROXY_PORT):
                logger.error(f"Port {PROXY_PORT} is already in use")
                return False
                
            if is_port_open(OLLAMA_REAL_PORT):
                logger.error(f"Port {OLLAMA_REAL_PORT} is already in use")
                return False
                
            logger.info("Starting Ollama server...")
            
            # Find and start Ollama
            ollama_exe = get_ollama_executable()
            env = os.environ.copy()
            env['OLLAMA_HOST'] = f'0.0.0.0:{OLLAMA_REAL_PORT}'
            
            self.ollama_process = subprocess.Popen(
                [ollama_exe, 'serve'],
                env=env,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                creationflags=subprocess.CREATE_NO_WINDOW if hasattr(subprocess, 'CREATE_NO_WINDOW') else 0
            )
            
            # Wait for Ollama to be ready
            if not wait_for_ollama(OLLAMA_REAL_PORT):
                logger.error("Failed to start Ollama server")
                return False
                
            logger.info(f"Ollama server started on port {OLLAMA_REAL_PORT}")
            
            # Start the proxy in a separate thread
            self.proxy_thread = threading.Thread(target=self._run_proxy, daemon=True)
            self.proxy_thread.start()
            
            # Start system tray icon
            self.tray_icon = SystemTrayIcon(self)
            self.tray_thread = threading.Thread(target=self.tray_icon.start_tray, daemon=True)
            self.tray_thread.start()
            
            logger.info(f"Service started successfully - Proxy on port {PROXY_PORT}")
            return True
            
        except Exception as e:
            logger.error(f"Failed to start service: {e}")
            return False
            
    def _run_proxy(self):
        """Run the proxy server"""
        try:
            from ollama_hybrid_proxy import HybridOllamaProxy
            
            proxy = HybridOllamaProxy(
                ollama_host=f'http://localhost:{OLLAMA_REAL_PORT}',
                proxy_port=PROXY_PORT,
                analytics_backend='sqlite'
            )
            
            logger.info(f"Starting proxy on port {PROXY_PORT}")
            proxy.run()
            
        except Exception as e:
            logger.error(f"Proxy error: {e}")
            
    def stop_service(self):
        """Stop the service"""
        logger.info("Stopping Ollama Metrics Proxy Service")
        self.running = False
        
        # Stop system tray
        if self.tray_icon:
            self.tray_icon.stop_tray()
            
        # Stop Ollama process
        if self.ollama_process:
            try:
                self.ollama_process.terminate()
                self.ollama_process.wait(timeout=10)
                logger.info("Ollama process stopped")
            except Exception as e:
                logger.error(f"Error stopping Ollama process: {e}")
                
        logger.info("Service stopped")


def run_console():
    """Run in console mode for testing"""
    print("=" * 60)
    print("  Ollama Metrics Proxy Service (Console Mode)")
    print("=" * 60)
    print(f"Log file: {log_file}")
    print("Press Ctrl+C to stop")
    print("=" * 60)
    
    service = OllamaProxyService()
    
    try:
        if service.start_service():
            print("✓ Service started successfully")
            print(f"✓ Proxy running on http://localhost:{PROXY_PORT}")
            print(f"✓ Metrics at http://localhost:{PROXY_PORT}/metrics")
            print(f"✓ Analytics at http://localhost:{PROXY_PORT}/analytics/stats")
            
            # Keep running
            while service.running:
                time.sleep(1)
        else:
            print("✗ Failed to start service")
            
    except KeyboardInterrupt:
        print("\nShutting down...")
    finally:
        service.stop_service()


if __name__ == '__main__':
    if len(sys.argv) == 1:
        # No arguments - run in console mode
        run_console()
    elif WINDOWS_SERVICE_AVAILABLE:
        # Handle service operations
        win32serviceutil.HandleCommandLine(OllamaProxyService)
    else:
        print("Windows service modules not available. Running in console mode.")
        run_console()