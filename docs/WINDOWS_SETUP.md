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

Download the latest installer directly from GitHub Releases:

```text
CodexQuotaGuardSetup-vX.Y.Z.exe
```

Double-click it and complete the normal Inno Setup wizard.

No ZIP extraction, PowerShell installer, or execution-policy change is required.

After installation:

1. Fully quit and reopen Codex Desktop once so it reloads the MCP companion.
2. Open a new terminal.
3. Verify:

```powershell
orch status
orch ui
```

## What setup installs

```text
%LOCALAPPDATA%\CodexQuotaGuard\
├─ unins000.exe
├─ config.example.json
├─ docs\
└─ bin\
   ├─ orchestrator.exe
   ├─ orchestrator-daemon.exe
   ├─ orch.exe
   └─ desktop-companion.exe
```

Inno Setup handles normal Windows installation concerns:

- copies application files;
- adds `%LOCALAPPDATA%\CodexQuotaGuard\bin` to the current User PATH;
- registers per-user background startup through the Windows Run key;
- creates the Windows Installed Apps entry and standard uninstaller;
- uses the Windows Restart Manager when installed files are in use.

Codex-specific integration is deliberately delegated to the application command:

```powershell
orch setup
```

During setup this command is run automatically. It:

- registers the `desktop-quota-guard` MCP companion through Codex CLI;
- adds/updates the managed Quota Guard block in `~/.codex/AGENTS.md`;
- initializes the relay when needed.

The daemon is built with the Windows GUI subsystem and background child processes use `CREATE_NO_WINDOW`, so normal operation should not keep or periodically flash terminal windows.

## Runtime data

State is kept separately from installed binaries:

```text
~\.codex-desktop-quota-guard\
```

This includes SQLite task history, relay state, and the optional shared `config.json`. Normal reinstall/upgrade and uninstall preserve it.

## Upgrade / repair

Download the newer:

```text
CodexQuotaGuardSetup-vX.Y.Z.exe
```

and run it again.

Inno Setup identifies the existing installation using the same application ID and upgrades the files in place. If a file is in use, Windows Restart Manager handles the normal close/retry flow rather than the installer force-killing processes itself.

After an upgrade, Codex bootstrap is refreshed automatically. To repair only the Codex integration later, run:

```powershell
orch setup
```

## Everyday commands

```powershell
orch setup
orch teardown
orch config show
orch config path
orch config validate
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

Settings:

```text
http://127.0.0.1:47631/settings.html
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

Use Windows Settings:

```text
Settings
→ Apps
→ Installed apps
→ Codex Desktop Quota Guard
→ Uninstall
```

or run the standard Inno Setup uninstaller:

```text
%LOCALAPPDATA%\CodexQuotaGuard\unins000.exe
```

Before installed files are removed, the uninstaller runs:

```powershell
orch teardown
```

which removes the MCP registration and the managed Quota Guard block in `AGENTS.md`.

The runtime data directory is intentionally left intact:

```text
~\.codex-desktop-quota-guard\
```

If a full data purge is desired, delete that directory manually after uninstalling.

## Configuration

Without `--config` or `CDQG_CONFIG`, the daemon, Desktop companion, and CLI all resolve the same optional configuration file:

```text
%USERPROFILE%\.codex-desktop-quota-guard\config.json
```

If that file does not exist, built-in defaults are used so existing installs continue working unchanged.

Defaults:

```text
listen:          127.0.0.1:47631
hard threshold:  5%
soft threshold:  10%
resume:          20%
quota poll:      60s
companion poll:  5s
request timeout: 20s
auto dispatch:   true
```

The normal way to change these values is the dashboard **Settings** page. It persists through:

```text
GET /v1/settings
PUT /v1/settings
```

The settings page can edit:

- hard / soft / resume quota thresholds;
- quota polling interval;
- Desktop companion polling interval;
- Codex request timeout;
- sequential project queue auto-dispatch;
- daemon listen address.

The file is written through a temporary file and replaced only after the new JSON has been fully written. Saved settings currently require a daemon restart, and Codex Desktop should be reopened so the companion reloads the same shared config.

Inspect the effective config from a terminal:

```powershell
orch config show
orch config path
orch config validate
```

Explicit config paths remain supported:

```powershell
orch status --config C:\path\to\config.json
orch config --config C:\path\to\config.json show
orch daemon --config C:\path\to\config.json
```

Advanced/development environment overrides remain available through `CDQG_CONFIG`, `CDQG_DATA_DIR`, and `CDQG_CODEX_COMMAND`.

## Antivirus / reputation note

The release installer is now built with Inno Setup rather than a custom self-copying Go installer. This removes custom installer behaviors such as self-copying to `Uninstall.exe`, manual uninstall registry management, and force-killing application processes.

The project still intends to add trusted code signing for release binaries. Until signed reputation is established, Windows Defender or SmartScreen may still occasionally warn on a new release. Do not solve that by disabling Defender or excluding broad folders; verify the published SHA256 and report false positives when needed.

## Source/development scripts

Legacy PowerShell scripts may remain in the repository for development and migration testing, but release users should use the Inno Setup installer and Windows Installed Apps.

## Important limitation

Pause remains cooperative at model/tool safe boundaries. The project does not claim a stable hard-interrupt API for arbitrary active Codex Desktop turns.
