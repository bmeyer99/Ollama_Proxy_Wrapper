# Installation Architecture

## Overview
The installation system has been refactored for clarity, maintainability, and efficiency.

## Core Components

### 1. **install_utils.bat** - Shared Utilities
Central library of reusable functions:
- `ensure_script_dir` - Ensures scripts run from correct directory
- `create_log` - Creates timestamped log files
- `log` - Outputs to both screen and log file
- `check_python` - Validates Python installation
- `check_admin` - Verifies administrator privileges
- `check_port` - Tests port availability
- `install_packages` - Installs Python packages with fallback
- `verify_package` - Confirms package installation
- `check_files` - Validates required files exist
- `stop_service` - Stops existing services
- `create_launcher` - Creates ollama_metrics.bat
- `test_install` - Validates installation

### 2. Main Installation Scripts

#### **quick_install.bat**
- User-friendly basic installation
- No admin rights required
- Installs Python packages
- Creates launcher script
- Clear error messages and logging

#### **install_service.bat**
- Windows service installation
- Requires administrator privileges
- Auto-detects Microsoft Store Python
- Falls back to alternative installation when needed
- Comprehensive error handling

#### **install_service_alternative.bat**
- Fallback for Microsoft Store Python
- Uses SC.exe instead of Python service framework
- Called automatically by install_service.bat

### 3. Management Scripts

#### **diagnose_install.bat**
- Comprehensive system diagnostics
- Checks all prerequisites
- Identifies common issues
- Creates detailed report

#### **uninstall_service.bat**
- Clean service removal
- Works for both installation types
- Proper cleanup and verification

#### **test_service.bat**
- Runs proxy in console mode
- Useful for debugging
- Shows real-time output

## Key Improvements

1. **DRY Principle**: Common functionality centralized in install_utils.bat
2. **Consistent Logging**: All scripts create timestamped logs with dual output
3. **Better Error Handling**: Clear messages and proper exit codes
4. **Directory Independence**: Scripts work correctly regardless of working directory
5. **Microsoft Store Python**: Automatic detection and handling
6. **Port Conflict Detection**: Prevents installation issues
7. **Clean Architecture**: Each script has a single, clear purpose

## Installation Flow

```
1. User runs quick_install.bat
   → Loads install_utils.bat
   → Checks Python
   → Checks port 11434
   → Installs packages
   → Creates launcher
   → Tests installation

2. User runs install_service.bat (as admin)
   → Loads install_utils.bat
   → Verifies admin rights
   → Checks Python type
   → If Microsoft Store Python:
     → Calls install_service_alternative.bat
   → Else:
     → Standard pywin32 installation
   → Configures service
   → Starts service
```

## File Structure
```
/
├── install_utils.bat              # Shared utilities
├── quick_install.bat              # Basic installation
├── install_service.bat            # Service installation
├── install_service_alternative.bat # MS Store Python fallback
├── diagnose_install.bat           # Diagnostic tool
├── uninstall_service.bat          # Service removal
├── test_service.bat               # Console mode testing
└── ollama_metrics.bat             # Created by installer
```

## Usage

### First-time Installation
```batch
quick_install.bat
```

### Service Installation
```batch
REM Right-click → Run as administrator
install_service.bat
```

### Troubleshooting
```batch
diagnose_install.bat
```

### Testing
```batch
test_service.bat
```

## Notes

- All scripts handle spaces in paths correctly
- Logs are created with timestamps for debugging
- Microsoft Store Python is automatically detected
- Port conflicts are checked before installation
- Service recovery is configured for automatic restart