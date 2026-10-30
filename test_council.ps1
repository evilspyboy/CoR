$proc = Start-Process -FilePath ".\council.exe" -ArgumentList "--no-browser" -PassThru
Start-Sleep -Milliseconds 1000

try {
    Write-Host "Probing /api/state..."
    $state = Invoke-RestMethod -Uri "http://127.0.0.1:8765/api/state"
    Write-Host "State API OK! Universes:" ($state.universes.Count)

    Write-Host "Probing /mcp (tools/list)..."
    $body = '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
    $mcp = Invoke-RestMethod -Uri "http://127.0.0.1:8765/mcp" -Method Post -Body $body -ContentType "application/json"
    Write-Host "MCP API OK! Tools registered:" ($mcp.result.tools.Count)
    foreach ($tool in $mcp.result.tools) {
        Write-Host " - Tool:" $tool.name
    }
} finally {
    Write-Host "Stopping test process..."
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
}

