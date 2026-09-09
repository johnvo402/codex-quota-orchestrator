param(
    [switch]$SkipRelayInit,
    [switch]$SkipAgentInstructions
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$bin = Join-Path $root 'bin'
New-Item -ItemType Directory -Force $bin | Out-Null

Write-Host '==> Checking prerequisites'
go version
codex --version

Write-Host '==> Restoring Go modules'
Push-Location $root
try {
    go mod tidy
    go test ./...
    go vet ./...
    go build -o (Join-Path $bin 'orchestrator.exe') ./cmd/orchestrator
    go build -o (Join-Path $bin 'desktop-companion.exe') ./cmd/desktop-companion
} finally { Pop-Location }

$companion = (Resolve-Path (Join-Path $bin 'desktop-companion.exe')).Path
$orchestrator = (Resolve-Path (Join-Path $bin 'orchestrator.exe')).Path

Write-Host '==> Registering MCP companion globally in Codex'
try { & codex mcp remove desktop-quota-guard 2>$null | Out-Null } catch {}
& codex mcp add desktop-quota-guard -- $companion

if (-not $SkipAgentInstructions) {
    $codexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME '.codex' }
    New-Item -ItemType Directory -Force $codexHome | Out-Null
    $agents = Join-Path $codexHome 'AGENTS.md'
    $start = '<!-- desktop-quota-guard:start -->'
    $end = '<!-- desktop-quota-guard:end -->'
    $block = @"
$start
## Desktop Quota Guard
For substantial coding tasks in Codex Desktop:
- Near the beginning, call the `desktop_task_register` tool from the `desktop-quota-guard` MCP server with a concise objective and current workspace.
- Before a substantial new phase, and at natural safe boundaries during long work, call `quota_check`. Do not call it before every tiny read/edit/command.
- If `quota_check` returns `action=pause`, do not start new work. Finish only the current atomic operation, call `task_checkpoint` with completed/pending/test state, then call `task_mark_paused` and end the turn.
- When a `[Desktop Quota Guard] Quota is available again` message arrives, continue from the existing thread/checkpoint and call `quota_check` again before another long phase.
- When the objective is complete, call `task_complete`.
$end
"@
    $existing = if (Test-Path $agents) { Get-Content $agents -Raw } else { '' }
    $pattern = [regex]::Escape($start) + '(?s).*?' + [regex]::Escape($end)
    if ($existing -match $pattern) { $new = [regex]::Replace($existing, $pattern, $block) }
    else { $new = ($existing.TrimEnd() + "`r`n`r`n" + $block + "`r`n").TrimStart() }
    Set-Content -Path $agents -Value $new -Encoding UTF8
    Write-Host "Updated $agents"
}

if (-not $SkipRelayInit) {
    Write-Host '==> Creating one persistent relay executor thread (one tiny Codex turn)'
    & $orchestrator relay-init
}

Write-Host ''
Write-Host 'Installed.'
Write-Host "Start daemon in a terminal:"
Write-Host "  & '$orchestrator' daemon"
Write-Host 'Then fully restart Codex Desktop so it reloads the MCP server.'
Write-Host "Run: & '$orchestrator' doctor"
