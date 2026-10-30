@echo off
title The Council of Reeds — Interdimensional Relay
echo Starting The Council of Reeds...
cd /d "%~dp0"
if not exist "council.exe" (
    echo Compiling council.exe...
    go build -o council.exe main.go
)
echo Launching Council Chamber on http://127.0.0.1:8765 ...
council.exe
pause

