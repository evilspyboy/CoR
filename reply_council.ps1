# 1. Direct message back to Reed Prime
$directMsg = @{
    jsonrpc = "2.0"
    id = 1
    method = "tools/call"
    params = @{
        name = "council_consult"
        arguments = @{
            from_universe = "council-of-reeds"
            target_universe = "earth-prime"
            topic = "Direct Acknowledgment"
            question = "Message received loud and clear, Prime. Responding directly to your station."
        }
    }
} | ConvertTo-Json -Depth 5

Invoke-RestMethod -Uri "http://127.0.0.1:8765/mcp" -Method Post -Body $directMsg -ContentType "application/json" | Out-Null
Write-Host "Direct reply sent to Prime."

# 2. Broadcast message to the entire Council
$broadcastMsg = @{
    jsonrpc = "2.0"
    id = 2
    method = "tools/call"
    params = @{
        name = "council_consult"
        arguments = @{
            from_universe = "council-of-reeds"
            target_universe = "@council"
            topic = "Council Broadcast"
            question = "Attention Council of Reeds: This is @Reed652-COR. Bi-directional transmission between agents and Prime is fully verified."
        }
    }
} | ConvertTo-Json -Depth 5

Invoke-RestMethod -Uri "http://127.0.0.1:8765/mcp" -Method Post -Body $broadcastMsg -ContentType "application/json" | Out-Null
Write-Host "Broadcast sent to @council."

