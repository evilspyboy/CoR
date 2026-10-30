$convId = "6c4f6aea-0dbc-482e-84d5-50aec55ac185"
$wsPath = "c:\tcht\council of reeds"

Write-Host "Looking up agentapi executable..."
$agentapi = "C:\Users\andre\.gemini\antigravity-ide\scratch\antigravity-orchestrator\bin\agentapi.exe"
if (-not (Test-Path $agentapi)) {
    $psOut = Get-CimInstance Win32_Process -Filter "Name = 'language_server_windows_x64.exe'" | Select-Object -ExpandProperty CommandLine
    if ($psOut -match '([A-Za-z]:\\[^"]+?\\language_server_windows_x64\.exe)') {
        $dir = Split-Path $matches[1]
        $cand = Join-Path $dir "agentapi.exe"
        if (Test-Path $cand) { $agentapi = $cand }
    }
}
Write-Host "AgentAPI path: $agentapi"

Write-Host "Reading IPC hooks..."
$ipcJsonPath = "C:\Users\andre\.gemini\antigravity-ide\scratch\antigravity-orchestrator\ipc_hooks.json"
$hook = $null
if (Test-Path $ipcJsonPath) {
    $hooks = Get-Content $ipcJsonPath -Raw | ConvertFrom-Json
    $norm = $wsPath.ToLower().Replace("\", "/")
    if ($hooks.$norm) {
        $hook = $hooks.$norm.ipc_hook
    }
}
Write-Host "IPC Hook: $hook"

$env:VSCODE_IPC_HOOK = $hook
$msg = "[Council of Reeds] Self-test transmission received across the sub-space relay."

Write-Host "Executing agentapi send-message..."
& $agentapi agentapi send-message $convId $msg
Write-Host "Exit code: $LASTEXITCODE"
