@echo off
title The Council of Reeds — Interdimensional Relay
echo Starting The Council of Reeds...
cd /d "%~dp0"

echo Stopping any running council instances...
taskkill /f /im council.exe >nul 2>&1
timeout /t 1 /nobreak >nul

if exist "council_staged.exe" (
    echo Applying staged update...
    del /f /q "council.exe" >nul 2>&1
    move /y "council_staged.exe" "council.exe"
    if errorlevel 1 (
        echo [ERROR] Failed to replace council.exe with council_staged.exe!
        pause
        exit /b 1
    )
    echo Staged update applied successfully.
)

if not exist "council.exe" (
    echo Compiling council.exe...
    go build -ldflags="-H=windowsgui" -o council.exe main.go
)

echo Launching Council Chamber on http://127.0.0.1:8765 ...
start "" council.exe

