@echo off
:: Launch PowerShell installer with admin rights
powershell -ExecutionPolicy Bypass -Command "Start-Process powershell -ArgumentList '-ExecutionPolicy Bypass -File ""%~dp0Install-Service.ps1""' -Verb RunAs"