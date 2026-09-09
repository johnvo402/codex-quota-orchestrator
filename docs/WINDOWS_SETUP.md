# Windows setup

## Prerequisites

For a packaged GitHub release:

- Codex CLI installed and logged in with ChatGPT
- Codex Desktop
- Windows PowerShell 5.1+ or PowerShell 7+

For source builds, Go 1.23+ is also required.

Verify:

```powershell
codex --version
```

For source builds:

```powershell
go version
```

## Install

From an extracted release or project root:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-windows.ps1
```

The installer installs stable copies of the binaries here:

```text
%LOCALAPPDATA%\CodexQuotaGuard\bin\orchestrator.exe
%LOCALAPPDATA%\CodexQuotaGuard\bin\desktop-companion.exe
```

It then:

- registers `desktop-quota-guard` through `codex mcp add` using the installed companion path;
- adds or updates the marked Quota Guard block in `~/.codex/AGENTS.md`;
- creates the relay executor thread if one is not already persisted;
- creates the per-user Scheduled Task `Codex Desktop Quota Guard`;
- starts the daemon immediately.

The default local state remains under:

```text
~\.codex-desktop-quota-guard\
```

This keeps SQLite state and relay configuration separate from installed binaries, so reinstalling or upgrading does not wipe task history.

Fully quit Codex Desktop and reopen it after installation so the Desktop app reloads the MCP configuration.

### Installer options

```powershell
.\scripts\install-windows.ps1 -SkipRelayInit
.\scripts\install-windows.ps1 -SkipAgentInstructions
.\scripts\install-windows.ps1 -SkipAutoStart
.\scripts\install-windows.ps1 -ForceBuild
```

`-ForceBuild` is intended for source development. A release package already contains the Windows binaries.

## Autostart

The installer creates this per-user Scheduled Task:

```text
Codex Desktop Quota Guard
```

Inspect it:

```powershell
Get-ScheduledTask -TaskName 'Codex Desktop Quota Guard'
```

Start it manually if needed:

```powershell
Start-ScheduledTask -TaskName 'Codex Desktop Quota Guard'
```

The task is configured with `MultipleInstances IgnoreNew`.

The daemon itself also has single-instance behavior by listen address. Starting it twice is safe when the existing process is a healthy Quota Guard daemon:

```powershell
& "$env:LOCALAPPDATA\CodexQuotaGuard\bin\orchestrator.exe" daemon
```

The second invocation reports that the daemon is already running and exits successfully.

## Everyday operation

Define a convenient PowerShell variable:

```powershell
$oq = "$env:LOCALAPPDATA\CodexQuotaGuard\bin\orchestrator.exe"
```

Overall status:

```powershell
& $oq status
```

List tracked Desktop tasks:

```powershell
& $oq list
```

Check account/rate-limit diagnostics:

```powershell
& $oq doctor
```

Show installed version:

```powershell
& $oq version
```

`doctor` runs outside the Codex Desktop process tree, so it intentionally does not claim native Desktop-pipe reachability. Use the MCP tool `desktop_guard_status` from inside a Desktop task to validate native delivery.

## Pause/resume behavior

When quota becomes low, `quota_check` returns `action=pause`.

The expected cooperative flow is:

```text
RUNNING
→ PAUSE_REQUESTED
→ checkpoint
→ PAUSED_QUOTA
```

When both relevant quota windows are usable again:

```text
PAUSED_QUOTA
→ RESUME_QUEUED
→ native Desktop send
→ RUNNING
```

The continuation is sent to the same Codex Desktop thread through the native Desktop tools pipe.

## Manual recovery

A task can become `NEEDS_REVIEW` when a native resume send was attempted but its outcome cannot be proven. This is intentionally conservative: the guard will not automatically send another continuation because the first message may already have reached Desktop.

First inspect the destination thread, then choose:

```powershell
& $oq recover --thread <threadId> --resolution retry
& $oq recover --thread <threadId> --resolution running
& $oq recover --thread <threadId> --resolution cancel
```

Use `retry` only when you confirmed that the previous continuation did not arrive.

See `TROUBLESHOOTING.md` for details.

## Configuration

Defaults:

```text
listen:          127.0.0.1:47631
hard threshold:  5%
soft threshold:  10%
resume:          20%
quota poll:      60s
companion poll:  5s
```

Thresholds must satisfy:

```text
0 <= hard < soft < resume <= 100
```

Copy `config.example.json` and pass it with `--config` for manual runs, or use the supported `CDQG_*` environment variables.

For the standard installed autostart configuration, the defaults are recommended unless you intentionally customize the Scheduled Task arguments/environment too.

## Upgrade

For a new release, extract the new ZIP and run the installer again:

```powershell
.\scripts\install-windows.ps1
```

It replaces the installed binaries, refreshes the MCP registration and Scheduled Task, and preserves `~/.codex-desktop-quota-guard` state.

Restart Codex Desktop after the upgrade.

## Uninstall

Remove the Scheduled Task, MCP registration, managed AGENTS block and installed binaries while keeping local task state:

```powershell
.\scripts\uninstall-windows.ps1
```

Also delete SQLite state and relay configuration:

```powershell
.\scripts\uninstall-windows.ps1 -PurgeData
```

## Important limitation

This version does not claim a stable hard-interrupt API for arbitrary active Codex Desktop turns. `PAUSE_REQUESTED` is cooperative and should not be treated as a safety boundary for destructive operations.
