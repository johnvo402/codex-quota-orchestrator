# Windows setup

## Prerequisites

For a packaged release:

- Windows 10/11 x64
- Codex Desktop
- Codex CLI installed and logged in

Verify:

```powershell
codex --version
```

Go is only required for source development.

## Install from GitHub Release

1. Download the latest `codex-quota-orchestrator-<version>-windows-x64.zip` from GitHub Releases.
2. Extract the ZIP.
3. Double-click:

```text
CodexQuotaGuardSetup.exe
```

4. Confirm installation.
5. Fully quit and reopen Codex Desktop once so it reloads the MCP companion.
6. Open a new terminal and verify:

```powershell
orch status
orch ui
```

No PowerShell installer and no execution-policy change are required.

## What setup installs

```text
%LOCALAPPDATA%\CodexQuotaGuard\
├─ Uninstall.exe
└─ bin\
   ├─ orchestrator.exe
   ├─ orchestrator-daemon.exe
   ├─ orch.exe
   └─ desktop-companion.exe
```

Setup also:

- adds `%LOCALAPPDATA%\CodexQuotaGuard\bin` to the current User PATH;
- registers `desktop-quota-guard` through Codex CLI;
- adds/updates the managed Quota Guard block in `~/.codex/AGENTS.md`;
- initializes the relay when needed;
- registers per-user background startup through the Windows Run key;
- registers **Codex Desktop Quota Guard** in Windows Installed Apps;
- starts `orchestrator-daemon.exe` immediately in the background.

The daemon is built with the Windows GUI subsystem and background child processes use `CREATE_NO_WINDOW`, so normal operation should not keep or periodically flash terminal windows.

## Runtime data

State is kept separately from installed binaries:

```text
~\.codex-desktop-quota-guard\
```

This includes SQLite task history and relay state. Normal reinstall/upgrade preserves it.

## Upgrade / repair

Download/extract the newer release and double-click its:

```text
CodexQuotaGuardSetup.exe
```

Before modifying the existing installation, setup checks whether Codex Desktop is still using `desktop-companion.exe`.

If it is, setup asks you to:

```text
Fully quit Codex Desktop
→ click Retry
```

This preflight happens before replacing installed files, preventing the previous partial-upgrade/file-lock failure mode.

Setup then refreshes binaries, MCP registration, PATH, startup registration and Windows uninstall metadata while preserving runtime state.

## Everyday commands

```powershell
orch status
orch ui
orch list
orch doctor
orch version
```

Dashboard:

```text
http://127.0.0.1:47631/
```

If `orch` is not recognized immediately after installation, open a new terminal so it receives the updated User PATH.

## Background startup

At Windows login, the installer starts:

```text
%LOCALAPPDATA%\CodexQuotaGuard\bin\orchestrator-daemon.exe daemon
```

through the current user's Windows Run key.

When Codex Desktop launches `desktop-companion.exe`, the companion also checks:

```text
http://127.0.0.1:47631/healthz
```

and starts the hidden daemon if it is not already healthy.

## Uninstall

### Windows Settings

```text
Settings
→ Apps
→ Installed apps
→ Codex Desktop Quota Guard
→ Uninstall
```

### Direct EXE

Double-click:

```text
%LOCALAPPDATA%\CodexQuotaGuard\Uninstall.exe
```

The uninstaller removes MCP registration, User PATH, background startup, legacy Scheduled Task registrations, the managed `AGENTS.md` block, installed binaries and the Installed Apps entry.

By default runtime data is preserved. Interactive uninstall asks whether to purge it too.

Silent uninstall:

```powershell
& "$env:LOCALAPPDATA\CodexQuotaGuard\Uninstall.exe" uninstall --silent
```

Silent uninstall plus data purge:

```powershell
& "$env:LOCALAPPDATA\CodexQuotaGuard\Uninstall.exe" uninstall --silent --purge-data
```

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

Manual CLI commands support `--config`, for example:

```powershell
orch status --config C:\path\to\config.json
orch daemon --config C:\path\to\config.json
```

Background configuration currently uses defaults plus supported `CDQG_*` environment variables. Installer-level config selection is planned separately.

## Source/development scripts

Legacy PowerShell scripts may remain in the repository for development and migration testing, but release users should use `CodexQuotaGuardSetup.exe` and Windows Installed Apps / `Uninstall.exe`.

## Important limitation

Pause remains cooperative at model/tool safe boundaries. The project does not claim a stable hard-interrupt API for arbitrary active Codex Desktop turns.
