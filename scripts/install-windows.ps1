param(
    [switch]$SkipRelayInit,
    [switch]$SkipAgentInstructions,
    [switch]$SkipAutoStart,
    [switch]$SkipPath,
    [switch]$ForceBuild
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$sourceBin = Join-Path $root 'bin'
$installRoot = Join-Path $env:LOCALAPPDATA 'CodexQuotaGuard'
$installBin = Join-Path $installRoot 'bin'
$taskName = 'Codex Desktop Quota Guard'

function Assert-Command([string]$Name) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required command '$Name' was not found in PATH."
    }
}

function Add-UserPath([string]$PathToAdd) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @()
    if ($current) {
        $parts = @($current -split ';' | Where-Object { $_ -and $_.Trim() })
    }
    $normalized = $PathToAdd.TrimEnd('\')
    $exists = $false
    foreach ($part in $parts) {
        if ($part.Trim().TrimEnd('\') -ieq $normalized) {
            $exists = $true
            break
        }
    }
    if (-not $exists) {
        $newPath = if ($current) { "$current;$PathToAdd" } else { $PathToAdd }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    }
    if (-not (($env:Path -split ';') | Where-Object { $_.Trim().TrimEnd('\') -ieq $normalized })) {
        $env:Path = "$PathToAdd;$env:Path"
    }
}

Write-Host '==> Checking prerequisites'
Assert-Command 'codex'
& codex --version

$orchestratorSource = Join-Path $sourceBin 'orchestrator.exe'
$companionSource = Join-Path $sourceBin 'desktop-companion.exe'
$needBuild = $ForceBuild -or -not (Test-Path $orchestratorSource) -or -not (Test-Path $companionSource)

if ($needBuild) {
    Assert-Command 'go'
    Write-Host '==> Building from source'
    New-Item -ItemType Directory -Force $sourceBin | Out-Null
    Push-Location $root
    try {
        go test ./...
        if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
        go vet -unsafeptr=false ./...
        if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
        go build -o $orchestratorSource ./cmd/orchestrator
        if ($LASTEXITCODE -ne 0) { throw 'orchestrator build failed' }
        go build -o $companionSource ./cmd/desktop-companion
        if ($LASTEXITCODE -ne 0) { throw 'desktop-companion build failed' }
    } finally {
        Pop-Location
    }
}

Write-Host "==> Installing binaries to $installBin"
New-Item -ItemType Directory -Force $installBin | Out-Null
Copy-Item $orchestratorSource (Join-Path $installBin 'orchestrator.exe') -Force
Copy-Item $orchestratorSource (Join-Path $installBin 'orch.exe') -Force
Copy-Item $companionSource (Join-Path $installBin 'desktop-companion.exe') -Force

$orchestrator = (Resolve-Path (Join-Path $installBin 'orchestrator.exe')).Path
$companion = (Resolve-Path (Join-Path $installBin 'desktop-companion.exe')).Path

if (-not $SkipPath) {
    Write-Host '==> Adding CodexQuotaGuard bin directory to user PATH'
    Add-UserPath $installBin
}

Write-Host '==> Registering MCP companion globally in Codex'
try { & codex mcp remove desktop-quota-guard 2>$null | Out-Null } catch {}
& codex mcp add desktop-quota-guard -- $companion
if ($LASTEXITCODE -ne 0) { throw 'codex mcp add failed' }

if (-not $SkipAgentInstructions) {
    Write-Host '==> Installing global Codex instructions'
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
    if ($existing -match $pattern) {
        $new = [regex]::Replace($existing, $pattern, $block)
    } else {
        $new = ($existing.TrimEnd() + "`r`n`r`n" + $block + "`r`n").TrimStart()
    }
    Set-Content -Path $agents -Value $new -Encoding UTF8
    Write-Host "Updated $agents"
}

if (-not $SkipRelayInit) {
    Write-Host '==> Ensuring relay executor is initialized'
    $relayPath = Join-Path $HOME '.codex-desktop-quota-guard\relay.json'
    if (Test-Path $relayPath) {
        Write-Host "Relay already exists: $relayPath"
    } else {
        & $orchestrator relay-init
        if ($LASTEXITCODE -ne 0) { throw 'relay-init failed' }
    }
}

if (-not $SkipAutoStart) {
    Write-Host '==> Registering hidden daemon autostart task'
    $hiddenArgs = "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -Command `"& '$orchestrator' daemon`""
    $action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $hiddenArgs
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -MultipleInstances IgnoreNew
    Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Description 'Runs the local Codex Desktop Quota Guard daemon in the background.' -Force | Out-Null

    Write-Host '==> Starting daemon in background'
    Start-ScheduledTask -TaskName $taskName
    Start-Sleep -Milliseconds 800
}

Write-Host ''
Write-Host 'Installation complete.'
Write-Host "Installed binaries: $installBin"
Write-Host "Autostart task:      $taskName"
Write-Host "Dashboard:           http://127.0.0.1:47631/"
Write-Host ''
if (-not $SkipPath) {
    Write-Host "CLI alias installed: orch"
    Write-Host 'Open a new terminal if the orch command is not visible in an already-open shell.'
}
Write-Host 'The daemon runs hidden in the background. Use `orch status` to check it.'
Write-Host 'Fully quit and reopen Codex Desktop so it reloads the MCP server.'
Write-Host ''
Write-Host 'Useful commands:'
Write-Host '  orch ui'
Write-Host '  orch status'
Write-Host '  orch doctor'
Write-Host '  orch list'
Write-Host '  orch version'
