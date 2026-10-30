$reg = @{
    jsonrpc = "2.0"
    id = 1
    method = "tools/call"
    params = @{
        name = "council_register_variant"
        arguments = @{
            universe_id = "earth-antigravity"
            client_agent = "antigravity"
            model_family = "gemini-3.8-flash"
            power_tier = "WEAK"
            summary = "Core Council Architect & Framework Maintainer"
            conversation_id = "6c4f6aea-0dbc-482e-84d5-50aec55ac185"
            workspace_path = "c:\TCHT\Council of Reeds"
        }
    }
} | ConvertTo-Json -Depth 5

$response = Invoke-RestMethod -Uri "http://127.0.0.1:8765/mcp" -Method Post -Body $reg -ContentType "application/json"
Write-Output $response.result.content[0].text

