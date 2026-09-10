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
- creates the Windows Installed Apps entry and standard uninstaller;
- gracefully stops the installed quota daemon before replacing binaries;
- uses Windows Restart Manager for other installed files that may still be in use.

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

This includes SQLite task history, relay state, logs, and the optional shared `config.json`. Normal reinstall/upgrade and uninstall preserve it.

## Desktop-bound daemon lifecycle

The daemon is not registered in the current user's Windows Run key. It starts only when Codex Desktop launches the MCP companion and the companion needs the local guard service.

```text
Open Codex Desktop
   ↓
desktop-companion.exe starts
   ↓
companion checks /healthz
   ↓
starts orchestrator-daemon.exe when needed
   ↓
heartbeat lease keeps daemon alive
   ↓
Full Quit Codex Desktop
   ↓
companion disconnects / lease expires
   ↓
daemon gracefully exits
```

Multiple companion instances are safe. One companion exiting does not stop the daemon while another active lease remains.

A clean companion shutdown releases its lease immediately. If Codex Desktop or the companion crashes, the missing heartbeat expires after the grace period and the daemon exits automatically.

## Upgrade / repair

Download the newer:

```text
CodexQuotaGuardSetup-vX.Y.Z.exe
```

and run it again.

Before Inno Setup replaces installed binaries, it asks the installed CLI to stop the daemon:

```powershell
orch stop
```

This closes the HTTP listener, SQLite handle, logs, and process cleanly so `orchestrator-daemon.exe` is not locked during the upgrade.

Releases that predate `orch stop` cannot perform that graceful command themselves. For those legacy versions only, the installer uses runtime metadata to verify the recorded daemon identity and performs a narrowly scoped compatibility stop before replacing the old binary. Current-to-current upgrades use the graceful path.

After an upgrade, Codex bootstrap is refreshed automatically. The installer does not keep a standalone daemon running; opening/reopening Codex Desktop starts it on demand through `desktop-companion.exe`.

To repair only the Codex integration later, run:

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
orch restart
orch stop
orch logs
orch status
orch ui
orch list
orch doctor
orch version
```

Local UI:

```text
Dashboard:    http://127.0.0.1:47631/
Task Queue:   http://127.0.0.1:47631/queue.html
Settings:     http://127.0.0.1:47631/settings.html
Diagnostics:  http://127.0.0.1:47631/diagnostics.html
Desktop Stop: http://127.0.0.1:47631/stop.html
```

If `orch` is not recognized immediately after installation, open a new terminal so it receives the updated User PATH.

## Restarting and stopping the daemon

Use either the **Restart daemon** button on the Settings page or:

```powershell
orch restart
```

The restart is a graceful handoff:

```text
running daemon
   ↓
start hidden replacement child
   ↓
return HTTP 202 to Settings / CLI
   ↓
gracefully close HTTP server
   ↓
replacement sees old /healthz disappear
   ↓
replacement loads saved config and starts
```

`orch restart` waits until the replacement health endpoint is available before reporting success. This also handles the common case where Settings changes the listen port: the Settings page moves itself to the new local address once the daemon becomes healthy there.

To stop the daemon without starting a replacement:

```powershell
orch stop
```

When Codex Desktop is still open, its companion may start the daemon again because the guard is still needed. A full Codex Desktop quit removes the companion leases and leaves the daemon stopped.

If `companionPollSeconds` changes, fully reopen Codex Desktop after the daemon restart because `desktop-companion.exe` is a separate Codex-managed process.

## Desktop Stop

`Desktop Stop` is a Windows-only control for a managed task that is currently `RUNNING`. It does not send a chat prompt.

The guard records the exact `threadId` and `turnId`, navigates Codex Desktop to that thread, confirms through native `read_thread` that the same turn is still `inProgress`, then operates the unique visible Stop control.

An accessibility `InvokePattern` call alone is not considered success. Quota Guard reads the thread back and only marks the managed task `CANCELLED` after the exact expected turn reports `status=interrupted`.

If the first accessibility invocation leaves the same turn `inProgress`, Quota Guard can perform one guarded physical-click fallback after re-verifying the exact thread/turn and reacquiring the unique Stop button. The cursor is restored afterward.

If a Stop side effect was attempted but backend interruption cannot be confirmed, the durable action becomes `uncertain` and the managed task becomes `NEEDS_REVIEW`. It is never blindly clicked again.

See [`DESKTOP_STOP.md`](DESKTOP_STOP.md) for the detailed safety model.

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

Before installed files are removed, the uninstaller stops the quota daemon and runs:

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

If that file does not exist, built-in defaults are used.

Defaults:

```text
listen:          127.0.0.1:47631
hard threshold:  5%
soft threshold:  10%
5h resume:       20%
weekly resume:    5%
quota poll:      60s
companion poll:   5s
request timeout: 20s
auto dispatch:   true
```

The normal way to change these values is the dashboard **Settings** page. It persists through:

```text
GET /v1/settings
PUT /v1/settings
```

The settings page can edit:

- hard / soft pause thresholds;
- independent 5-hour and weekly resume thresholds;
- quota polling interval;
- Desktop companion polling interval;
- Codex request timeout;
- sequential project queue auto-dispatch;
- daemon listen address.

The file is written through a temporary file and replaced only after the new JSON has been fully written. If saved values differ from the running daemon, Settings exposes **Restart daemon**. The equivalent terminal command is:

```powershell
orch restart
```

Inspect the effective config from a terminal:

```powershell
orch config show
orch config path
orch config validate
```

Explicit config paths remain supported:

```powershell
orch status --config C:\path\to\config.json
orch restart --config C:\path\to\config.json
orch stop --config C:\path\to\config.json
orch config --config C:\path\to\config.json show
orch daemon --config C:\path\to\config.json
```

Advanced/development environment overrides remain available through `CDQG_CONFIG`, `CDQG_DATA_DIR`, and `CDQG_CODEX_COMMAND`.

## Antivirus / reputation note

The release installer is built with Inno Setup rather than a custom self-copying Go installer. This avoids custom installer behaviors such as self-copying to an uninstall executable, manual uninstall registry management, and broad force-killing of application processes.

The project still intends to add trusted code signing for release binaries. Until signed reputation is established, Windows Defender or SmartScreen may occasionally warn on a new release. Do not solve that by disabling Defender or excluding broad folders; verify the published SHA256 and report false positives when needed.

## Source/development scripts

Legacy PowerShell scripts may remain in the repository for development and migration testing, but release users should use the Inno Setup installer and Windows Installed Apps.

## Important limitations

Quota-driven pause remains cooperative at model/tool safe boundaries.

Desktop Stop depends on Codex Desktop's own Windows Stop path. The guard verifies the backend turn afterward, but if the current Codex Desktop build's Stop control itself is broken, Quota Guard cannot safely force a Desktop-owned turn through a separate competing app-server writer. It reports `NEEDS_REVIEW` instead of false success.
