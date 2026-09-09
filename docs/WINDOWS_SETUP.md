# Windows setup

## Prerequisites

For a packaged GitHub release:

- Codex CLI installed and logged in with ChatGPT
- Codex Desktop
- Windows 10/11 x64

Verify:

```powershell
codex --version
```

Go is only required when building from source.

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

No PowerShell installer script is required.

## What setup installs

Stable files are copied to:

```text
%LOCALAPPDATA%\CodexQuotaGuard\bin\orchestrator.exe
%LOCALAPPDATA%\CodexQuotaGuard\bin\orchestrator-daemon.exe
%LOCALAPPDATA%\CodexQuotaGuard\bin\orch.exe
%LOCALAPPDATA%\CodexQuotaGuard\bin\desktop-companion.exe
%LOCALAPPDATA%\CodexQuotaGuard\Uninstall.exe
```

Setup also:

- adds `%LOCALAPPDATA%\CodexQuotaGuard\bin` to the current user's PATH;
- registers the `desktop-quota-guard` MCP companion through Codex CLI;
- adds/updates the managed Quota Guard block in `~/.codex/AGENTS.md`;
- initializes the relay thread when it does not exist;
- registers background startup through the current user's Windows Run key;
- registers **Codex Desktop Quota Guard** in Windows Installed Apps;
- starts `orchestrator-daemon.exe` immediately in the background.

The daemon binary is built with the Windows GUI subsystem and is launched with `CREATE_NO_WINDOW`, so normal background startup does not keep a terminal window open.

## Data directory

Runtime state remains separate from installed binaries:

```text
~\.codex-desktop-quota-guard\
```

This contains SQLite state and relay configuration. Reinstalling/upgrading keeps this directory.

## Everyday commands

Open dashboard:

```powershell
orch ui
```

Overall status:

```powershell
orch status
```

Tracked tasks:

```powershell
orch list
```

Quota/account diagnostics:

```powershell
orch doctor
```

Installed version:

```powershell
orch version
```

If `orch` is not recognized immediately after installation, open a new PowerShell/Windows Terminal session so it receives the updated User PATH.

## Background startup

There are two complementary startup paths.

### Windows login

Setup writes a per-user startup entry for:

```text
%LOCALAPPDATA%\CodexQuotaGuard\bin\orchestrator-daemon.exe daemon
```

This starts the daemon after you sign in without a visible console window.

### Codex Desktop fallback

When Codex Desktop launches `desktop-companion.exe`, the companion checks:

```text
http://127.0.0.1:47631/healthz
```

If the daemon is not healthy, it starts `orchestrator-daemon.exe` itself with no console window. The daemon still has single-instance behavior by listen address.

## Upgrade

Download/extract the newer release and double-click:

```text
CodexQuotaGuardSetup.exe
```

Setup behaves as install-or-upgrade. It removes old launcher registrations, stops old installed processes, replaces binaries, refreshes MCP/PATH/startup registration, preserves local state, and starts the new daemon.

If Codex Desktop currently has `desktop-companion.exe` open, setup attempts to stop the installed companion before replacing it. If Windows still reports a locked file, fully quit Codex Desktop and run setup again.

## Uninstall

### Windows Settings

Open:

```text
Settings
→ Apps
→ Installed apps
→ Codex Desktop Quota Guard
→ Uninstall
```

### Direct EXE

You can also double-click:

```text
%LOCALAPPDATA%\CodexQuotaGuard\Uninstall.exe
```

The uninstaller removes:

- MCP registration;
- User PATH entry;
- Windows background startup entry;
- old Scheduled Task registrations from earlier releases;
- managed `AGENTS.md` block;
- installed binaries;
- Windows Installed Apps registration.

By default, task history/state is preserved.

During interactive uninstall you can choose whether to also remove:

```text
~\.codex-desktop-quota-guard\
```

For unattended uninstall:

```powershell
"$env:LOCALAPPDATA\CodexQuotaGuard\Uninstall.exe" uninstall --silent
```

To also delete local state:

```powershell
"$env:LOCALAPPDATA\CodexQuotaGuard\Uninstall.exe" uninstall --silent --purge-data
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

Manual CLI commands still support `--config`, for example:

```powershell
orch status --config C:\path\to\config.json
orch daemon --config C:\path\to\config.json
```

The standard EXE installer currently configures background startup with the default config/environment. If a custom background config is required, prefer supported `CDQG_*` user environment variables until installer-level config selection is added.

## Important limitation

Pause remains cooperative at model/tool safe boundaries. The project does not claim a stable hard-interrupt API for arbitrary active Codex Desktop turns.
