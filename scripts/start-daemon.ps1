$root=Split-Path -Parent $PSScriptRoot
& (Join-Path $root 'bin\orchestrator.exe') daemon
