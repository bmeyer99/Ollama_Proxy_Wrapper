@echo off
REM Test Ollama Metrics Proxy Service in Console Mode
REM This runs the service in the foreground for testing

echo =========================================================
echo  Testing Ollama Metrics Proxy Service (Console Mode)
echo =========================================================
echo.
echo This will run the service in console mode for testing.
echo Press Ctrl+C to stop the service.
echo.
echo Check these URLs once started:
echo   Proxy: http://localhost:11434
echo   Metrics: http://localhost:11434/metrics  
echo   Analytics: http://localhost:11434/analytics/stats
echo.
echo Starting service...
echo =========================================================

python ollama_service.py

echo.
echo Service stopped.
pause