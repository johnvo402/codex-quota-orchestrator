param(
    [switch]$PurgeData
)

$ErrorActionPreference = 'Continue'
$taskName = 'Codex Desktop Quota Guard'
$installRoot = Join-Path $env:LOCALAPPDATA 'CodexQuotaGuard'
$dataDir = Join-Path $HOME '.codex-desktop-quota-guard'

Write-Host '==> Stopping and removing daemon autostart task'
try { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue } catch {}
try { Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue } catch {}

Write-Host '==> Removing MCP registration'
try { & codex mcp remove desktop-quota-guard 2>$null | Out-Null } catch {}

Write-Host '==> Removing managed AGENTS.md block'
$codexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME '.codex' }
$agents = Join-Path $codexHome 'AGENTS.md'
if (Test-Path $agents) {
    $text = Get-Content $agents -Raw
    $start = '<!-- desktop-quota-guard:start -->'
    $end = '<!-- desktop-quota-guard:end -->'
    $pattern = [regex]::Escape($start) + '(?s).*?' + [regex]::Escape($end)
    $text = [regex]::Replace($text, $pattern, '').Trim()
    Set-Content $agents $text -Encoding UTF8
}

Write-Host '==> Removing installed binaries'
if (Test-Path $installRoot) {
    Remove-Item $installRoot -Recurse -Force -ErrorAction SilentlyContinue
}

if ($PurgeData) {
    Write-Host '==> Removing local state and relay configuration'
    if (Test-Path $dataDir) {
        Remove-Item $dataDir -Recurse -Force -ErrorAction SilentlyContinue
    }
} else {
    Write-Host "State preserved: $dataDir"
    Write-Host 'Use -PurgeData to remove SQLite state and relay configuration too.'
}

Write-Host 'Uninstalled. Fully restart Codex Desktop to unload the MCP companion.'
