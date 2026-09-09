# Codex Desktop Quota Guard

Desktop-first quota-aware orchestration for Codex.

Codex Desktop stays the owner and UI for real work. The guard monitors ChatGPT/Codex quota, persists checkpoints and task state, cooperatively pauses work at safe boundaries, and sends a continuation back into the same Desktop thread when quota is available again.

## Current capabilities

- Separates the 5-hour and weekly Codex quota windows.
- Local Go daemon with SQLite persistence.
- Codex Desktop MCP companion receives the current Desktop `threadId`.
- Cooperative pause/checkpoint flow at safe model/tool boundaries.
- Native Windows Desktop pipe discovery and `send_message_to_thread` resume delivery.
- Crash-safe action claim semantics that avoid duplicate resume sends.
- `NEEDS_REVIEW` state when native delivery outcome is uncertain.
- Manual recovery CLI for uncertain tasks.
- Single-instance daemon behavior.
- Windows installer with stable `%LOCALAPPDATA%` binary paths and per-user autostart.
- Tagged GitHub releases package Windows x64 binaries, scripts and docs.

## Important limitation

There is no claimed stable hard-stop implementation for arbitrary active Desktop turns. Pause is cooperative and happens at model/tool safe boundaries.

## Install on Windows

### From a GitHub release

Download and extract the Windows x64 ZIP, then run:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-windows.ps1
```

The installer:

- installs binaries to `%LOCALAPPDATA%\CodexQuotaGuard\bin`;
- registers the `desktop-quota-guard` MCP companion globally;
- installs a managed block in `~/.codex/AGENTS.md`;
- creates the persistent relay executor if needed;
- creates the per-user Scheduled Task `Codex Desktop Quota Guard`;
- starts the daemon immediately.

Fully quit and reopen Codex Desktop after installation.

### From source

The same installer builds automatically when `bin\orchestrator.exe` and `bin\desktop-companion.exe` are not present:

```powershell
.\scripts\install-windows.ps1
```

Use `-ForceBuild` to rebuild even when binaries already exist.

## Everyday commands

Installed CLI:

```powershell
$oq = "$env:LOCALAPPDATA\CodexQuotaGuard\bin\orchestrator.exe"
& $oq status
& $oq list
& $oq doctor
& $oq version
```

`status` gives the practical overview: daemon state, latest 5-hour/weekly quota, thresholds, relay state and task counts.

Starting `orchestrator daemon` while the configured daemon is already healthy is safe; it reports that the daemon is already running and exits successfully.

## Manual recovery

If a task becomes `NEEDS_REVIEW`, first inspect the destination Codex Desktop thread. Then use one of:

```powershell
orchestrator recover --thread <threadId> --resolution retry
orchestrator recover --thread <threadId> --resolution running
orchestrator recover --thread <threadId> --resolution cancel
```

- `retry`: use only when the previous continuation definitely did not arrive. The task returns to `PAUSED_QUOTA` and can be queued again when quota is healthy.
- `running`: use when the continuation did arrive and the thread is already continuing.
- `cancel`: abandon the managed task.

The guard deliberately does not auto-retry an uncertain native send because Desktop may have accepted the message even if the response was lost.

## Default policy

- hard threshold: 5% remaining
- soft pause: 10% remaining
- auto resume: 20% remaining
- quota poll: every 60 seconds
- companion action poll: every 5 seconds

Override with `config.example.json`, `--config`, or supported `CDQG_*` environment variables.

## Project layout

```text
cmd/
  orchestrator/       daemon, quota doctor, status, recovery CLI, relay bootstrap
  desktop-companion/  MCP server launched by Codex Desktop
internal/
  codexquota/         quota-only app-server client
  daemon/             quota monitor + local control API
  desktop/            Windows native Desktop delivery
  mcpserver/          MCP tools + background action delivery
  store/              SQLite persistence + crash-safe action lifecycle
  domain/             task states
  quota/              deterministic policy
scripts/
  install-windows.ps1
  uninstall-windows.ps1
  start-daemon.ps1
.github/workflows/
  ci.yml
  release.yml
```

## Docs

- `docs/WINDOWS_SETUP.md` — installation and daily operation.
- `docs/TROUBLESHOOTING.md` — daemon, native delivery, autostart and `NEEDS_REVIEW` recovery.
- `docs/ARCHITECTURE.md` — Desktop-first design.

## Uninstall

Keep SQLite task history and relay state:

```powershell
.\scripts\uninstall-windows.ps1
```

Remove state too:

```powershell
.\scripts\uninstall-windows.ps1 -PurgeData
```
