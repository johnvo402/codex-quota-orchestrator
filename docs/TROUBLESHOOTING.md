# Troubleshooting

## Daemon already running

`orchestrator daemon` is single-instance by listen address. If the configured daemon is already healthy, a second invocation prints:

```text
Daemon already running at http://127.0.0.1:47631
```

and exits successfully.

If the port is occupied by another process, the command reports that the address cannot be bound. Check it with:

```powershell
Get-NetTCPConnection -LocalPort 47631 -State Listen |
  Select-Object LocalAddress, LocalPort, OwningProcess
```

Then inspect the PID:

```powershell
Get-Process -Id <PID>
```

## Check overall status

Installed builds live under:

```text
%LOCALAPPDATA%\CodexQuotaGuard\bin
```

Run:

```powershell
& "$env:LOCALAPPDATA\CodexQuotaGuard\bin\orchestrator.exe" status
```

This shows daemon state, last sampled 5-hour and weekly quota, configured thresholds, relay state, and managed task counts.

For raw Codex account/rate-limit diagnostics:

```powershell
& "$env:LOCALAPPDATA\CodexQuotaGuard\bin\orchestrator.exe" doctor
```

Native Desktop delivery can only be proven from inside a Codex Desktop task with `desktop_guard_status` because the native pipe belongs to the Desktop process tree.

## Runtime logs

The daemon and Desktop companion persist structured JSON Lines logs under:

```text
~\.codex-desktop-quota-guard\logs\
```

Start with:

```powershell
orch logs
```

Show only recent warnings/errors or errors:

```powershell
orch logs --tail 100 --level warn
orch logs --level error
```

Follow new records while reproducing a problem:

```powershell
orch logs --follow
```

Filter one background component:

```powershell
orch logs --component daemon
orch logs --component companion
```

Use `orch logs --json` when the original structured records are needed. See `docs/LOGGING.md` for rotation, filtering, and privacy details.

## Native delivery unavailable

Inside Codex Desktop, call `desktop_guard_status`.

Healthy native status should include:

```text
nativeConfigured=true
nativeReachable=true
```

If `nativeConfigured=false`, fully quit and reopen Codex Desktop after installation. Verify that the MCP registration points to the installed companion:

```powershell
codex mcp list
```

If the native pipe changes after a Desktop restart, the companion discovers the current pipe from its parent Codex app-server process; no pipe path should be hard-coded.

## Task is NEEDS_REVIEW

`NEEDS_REVIEW` means a resume send was attempted but the system cannot prove whether Desktop received it. The guard intentionally does not resend automatically because doing so could start duplicate work.

Find the thread:

```powershell
orchestrator list
```

Then choose one manual resolution.

If you checked the Desktop thread and the continuation did **not** arrive:

```powershell
orchestrator recover --thread <threadId> --resolution retry
```

This returns the task to `PAUSED_QUOTA`. The scheduler will create one fresh resume action when quota is resumable.

If the continuation did arrive and the task is already continuing:

```powershell
orchestrator recover --thread <threadId> --resolution running
```

If you want to abandon the task:

```powershell
orchestrator recover --thread <threadId> --resolution cancel
```

Do not use `retry` unless you have checked the destination thread first.

## Scheduled Task / autostart

Installer creates the per-user task:

```text
Codex Desktop Quota Guard
```

Inspect it:

```powershell
Get-ScheduledTask -TaskName 'Codex Desktop Quota Guard'
```

Start it manually:

```powershell
Start-ScheduledTask -TaskName 'Codex Desktop Quota Guard'
```

The task uses `MultipleInstances IgnoreNew`, so Windows should not launch overlapping daemon processes.

## Reset installation

Uninstall while keeping task history and relay state:

```powershell
.\scripts\uninstall-windows.ps1
```

Remove local SQLite state and relay configuration too:

```powershell
.\scripts\uninstall-windows.ps1 -PurgeData
```

Then install again and fully restart Codex Desktop.

## Important safety behavior

Pause remains cooperative at model/tool safe boundaries. This project does not claim a stable external hard-interrupt API for arbitrary active Codex Desktop turns.

For resume delivery, an uncertain native send is never automatically repeated. This favors duplicate-work prevention over aggressive recovery.
