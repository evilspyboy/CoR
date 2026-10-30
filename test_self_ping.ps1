$convId = "6c4f6aea-0dbc-482e-84d5-50aec55ac185"
$agy = "C:\Users\andre\.gemini\bin\agy.exe"

Write-Host "Testing agy.exe path:" $agy
if (Test-Path $agy) {
    Write-Host "Found agy.exe! Sending test message..."
    & $agy agentapi send-message $convId "[Council of Reeds] Sub-space frequency active! Proactive notification verified."
    Write-Host "Exit code:" $LASTEXITCODE
} else {
    Write-Host "agy.exe not found at $agy"
}
