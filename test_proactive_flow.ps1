$proc = Start-Process -FilePath ".\council.exe" -ArgumentList "--no-browser" -PassThru
Start-Sleep -Milliseconds 800

try {
    Write-Host "=== 1. Register Variant A (earth-sim) ==="
    $regA = @{
        jsonrpc = "2.0"
        id = 1
        method = "tools/call"
        params = @{
            name = "council_register_variant"
            arguments = @{
                universe_id = "earth-sim"
                client_agent = "antigravity"
                model_family = "gemini-flash"
                power_tier = "NOMINAL"
                summary = "Synthetic telemetry generator for load testing"
                conversation_id = "test-conv-sim"
            }
        }
    } | ConvertTo-Json -Depth 5
    $resA = Invoke-RestMethod -Uri "http://127.0.0.1:8765/mcp" -Method Post -Body $regA -ContentType "application/json"
    Write-Host "Variant A registered:" $resA.result.content[0].text

    Write-Host "`n=== 2. Register Variant B (earth-api) ==="
    $regB = @{
        jsonrpc = "2.0"
        id = 2
        method = "tools/call"
        params = @{
            name = "council_register_variant"
            arguments = @{
                universe_id = "earth-api"
                client_agent = "codex"
                model_family = "gpt-4o"
                power_tier = "WEAK"
                summary = "Core ingestion gateway and batch processor"
                conversation_id = "test-thread-codex"
            }
        }
    } | ConvertTo-Json -Depth 5
    $resB = Invoke-RestMethod -Uri "http://127.0.0.1:8765/mcp" -Method Post -Body $regB -ContentType "application/json"
    Write-Host "Variant B registered:" $resB.result.content[0].text

    Write-Host "`n=== 3. Earth-Sim Consults Earth-API ==="
    $consult = @{
        jsonrpc = "2.0"
        id = 3
        method = "tools/call"
        params = @{
            name = "council_consult"
            arguments = @{
                from_universe = "earth-sim"
                target_universe = "earth-api"
                topic = "Ingestion Header Spec"
                question = "Are you expecting X-Telemetry-Nonce in hex or base64?"
                context_snippet = "header.Set('X-Telemetry-Nonce', nonce)"
            }
        }
    } | ConvertTo-Json -Depth 5
    $resConsult = Invoke-RestMethod -Uri "http://127.0.0.1:8765/mcp" -Method Post -Body $consult -ContentType "application/json"
    Write-Host "Consultation dispatched:" $resConsult.result.content[0].text

    Write-Host "`n=== 4. Reed Prime Issues Gavel Decree ==="
    $decree = @{
        decree = "Standardize all telemetry nonce headers to base64url across all universes."
    } | ConvertTo-Json
    $resDecree = Invoke-RestMethod -Uri "http://127.0.0.1:8765/api/decree" -Method Post -Body $decree -ContentType "application/json"
    Write-Host "Decree status:" $resDecree.status

    Write-Host "`n=== 5. Check Chamber State ==="
    $state = Invoke-RestMethod -Uri "http://127.0.0.1:8765/api/state"
    Write-Host "Registered Universes in Chamber:" $state.universes.Count
    foreach ($u in $state.universes) {
        Write-Host " - " $u.universe_id " | Epoch:" $u.variant_epoch " | " $u.summary
    }

    Write-Host "`n=== All Council Tests Passed Successfully! ==="
} finally {
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
}
