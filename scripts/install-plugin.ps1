# Council of Reeds Plugin Installer for Antigravity IDE
$ErrorActionPreference = "Stop"

$repoDir = Split-Path -Parent $PSScriptRoot
$pluginSrc = Join-Path $repoDir "plugin"
$targetDir = Join-Path $env:USERPROFILE ".gemini\config\plugins\council-of-reeds"
$exePath = Join-Path $repoDir "council.exe"

Write-Host "=================================================="
Write-Host "     Council of Reeds - Plugin Installer          "
Write-Host "=================================================="

# 1. Check/Build council.exe
if (-not (Test-Path $exePath)) {
    Write-Host "Compiling council.exe..."
    Set-Location $repoDir
    go build -o council.exe main.go
}

# 2. Prepare target plugin directory
Write-Host "Deploying plugin to: $targetDir"
New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
Copy-Item -Path (Join-Path $pluginSrc "*") -Destination $targetDir -Recurse -Force

# 3. Write absolute path into deployed mcp_config.json
$escapedExe = $exePath.Replace("\", "\\")
$mcpJson = @"
{
  "mcpServers": {
    "council-of-reeds": {
      "command": "$escapedExe",
      "type": "stdio",
      "args": [
        "--mcp"
      ]
    }
  }
}
"@
Set-Content -Path (Join-Path $targetDir "mcp_config.json") -Value $mcpJson -Encoding UTF8

# 4. Enable plugin live via Language Server RPC if available
if ($env:ANTIGRAVITY_LS_ADDRESS -and $env:ANTIGRAVITY_CSRF_TOKEN) {
    Write-Host "Enabling plugin live via Language Server ($env:ANTIGRAVITY_LS_ADDRESS)..."
    try {
        $body = '{"userConfig":{"plugins":{"council-of-reeds":{"enabled":true}}}}'
        $uri = "http://$env:ANTIGRAVITY_LS_ADDRESS/exa.language_server_pb.LanguageServerService/JetboxWriteState"
        Invoke-RestMethod -Uri $uri -Method Post -Body $body -Headers @{
            "Content-Type" = "application/json"
            "x-codeium-csrf-token" = $env:ANTIGRAVITY_CSRF_TOKEN
        } | Out-Null
        Write-Host "Plugin enabled live!"
    } catch {
        Write-Host "Note: Could not enable live ($_); plugin will load on next restart."
    }
} else {
    Write-Host "Language server environment not active; plugin will be active on next Antigravity start."
}

Write-Host "`nInstallation Complete! The Council of Reeds plugin is ready."
Write-Host "Run 'start-council.bat' to launch the Watcher Lounge."

