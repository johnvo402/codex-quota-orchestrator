$ErrorActionPreference='Continue'
try { & codex mcp remove desktop-quota-guard } catch {}
$codexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME '.codex' }
$agents = Join-Path $codexHome 'AGENTS.md'
if (Test-Path $agents) {
    $text=Get-Content $agents -Raw
    $start='<!-- desktop-quota-guard:start -->';$end='<!-- desktop-quota-guard:end -->'
    $pattern=[regex]::Escape($start)+'(?s).*?'+[regex]::Escape($end)
    $text=[regex]::Replace($text,$pattern,'').Trim()
    Set-Content $agents $text -Encoding UTF8
}
Write-Host 'MCP registration and managed AGENTS block removed. Data under ~/.codex-desktop-quota-guard was left intact.'
