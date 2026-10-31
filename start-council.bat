@echo off
title The Council of Reeds — Interdimensional Relay
echo Starting The Council of Reeds...
cd /d "%~dp0"
if exist "council_staged.exe" (
    echo Applying staged update...
    move /y "council_staged.exe" "council.exe" >nul
)
if not exist "council.exe" (
    echo Compiling council.exe...
    go build -ldflags="-H=windowsgui" -o council.exe main.go
)
echo Launching Council Chamber on http://127.0.0.1:8765 ...
start "" council.exe

