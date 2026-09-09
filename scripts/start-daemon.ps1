$ErrorActionPreference = 'Stop'
$installed = Join-Path $env:LOCALAPPDATA 'CodexQuotaGuard\bin\orchestrator.exe'
$root = Split-Path -Parent $PSScriptRoot
$local = Join-Path $root 'bin\orchestrator.exe'

if (Test-Path $installed) {
    & $installed daemon
    exit $LASTEXITCODE
}

if (Test-Path $local) {
    & $local daemon
    exit $LASTEXITCODE
}

throw 'orchestrator.exe not found. Run scripts\install-windows.ps1 first.'
