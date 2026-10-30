$ErrorActionPreference = 'Stop'
$codexRoot = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE '.codex' }
$configPath = Join-Path $codexRoot 'config.toml'
$instructionsPath = Join-Path $codexRoot 'AGENTS.md'
$skillDirectory = Join-Path $codexRoot 'skills\council-of-reeds'
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
$utf8 = New-Object System.Text.UTF8Encoding($false)
$config = if (Test-Path -LiteralPath $configPath) { [IO.File]::ReadAllText($configPath) } else { '' }
$instructions = if (Test-Path -LiteralPath $instructionsPath) { [IO.File]::ReadAllText($instructionsPath) } else { '' }
$skillPath = Join-Path $skillDirectory 'SKILL.md'
$skill = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'SKILL.md'))
$globalBlock = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'global-instructions.md'))

# Refuse to overwrite an existing server definition silently.
if ($config -match '(?m)^\[mcp_servers\.council-of-reeds\]') {
    throw 'Council server already configured; inspect the existing definition before updating.'
}
if ($instructions.Contains('<!-- council-of-reeds:start -->')) {
    throw 'Council global instructions already installed; inspect before updating.'
}
if (Test-Path -LiteralPath $skillPath) {
    throw 'Council skill already exists; inspect before updating.'
}

New-Item -ItemType Directory -Path $skillDirectory -Force | Out-Null
foreach ($path in @($configPath, $instructionsPath)) {
    if (Test-Path -LiteralPath $path) {
        Copy-Item -LiteralPath $path -Destination "$path.council-backup-$stamp"
    }
}
$serverBlock = @'

[mcp_servers.council-of-reeds]
url = "http://127.0.0.1:8765/mcp"
startup_timeout_sec = 10
tool_timeout_sec = 40
'@
[IO.File]::WriteAllText($configPath, $config.TrimEnd() + "`r`n" + $serverBlock + "`r`n", $utf8)
[IO.File]::WriteAllText($instructionsPath, $instructions.TrimEnd() + "`r`n" + $globalBlock + "`r`n", $utf8)
[IO.File]::WriteAllText($skillPath, $skill, $utf8)
Write-Output "Installed Council MCP connection, global instructions, and skill under $codexRoot. Existing files backed up with suffix .council-backup-$stamp."
