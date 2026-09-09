# Codex Desktop Quota Guard

Desktop-first quota-aware orchestration and local task management for Codex.

Codex Desktop stays the owner and UI for real work. The guard monitors ChatGPT/Codex quota, persists checkpoints and task state, cooperatively pauses work at safe boundaries, sends a continuation back into the same Desktop thread when quota is available again, and now includes a local dashboard for project/task management.

## Current capabilities

- Separates the 5-hour and weekly Codex quota windows.
- Local Go daemon with SQLite persistence.
- Codex Desktop MCP companion receives the current Desktop `threadId`.
- Cooperative pause/checkpoint flow at safe model/tool boundaries.
- Native Windows Desktop pipe discovery and `send_message_to_thread` resume delivery.
- Crash-safe action claim semantics that avoid duplicate resume sends.
- `NEEDS_REVIEW` state when native delivery outcome is uncertain.
- Manual recovery from CLI or dashboard.
- Local web dashboard embedded directly in `orchestrator.exe`.
- Project/workspace grouping with automatic project creation from task workspace paths.
- Project CRUD and safe task management (edit metadata, pause, cancel, recover, archive/delete terminal tasks).
- Task event timeline and quota/task overview.
- Single-instance daemon behavior.
- Windows installer with stable `%LOCALAPPDATA%` binary paths and per-user autostart.
- Tagged GitHub releases package Windows x64 binaries, scripts and docs.

## Local dashboard

The daemon serves the dashboard on the same localhost endpoint as its API:

```text
http://127.0.0.1:47631/
```

Open it with:

```powershell
orchestrator ui
```

or after installation:

```powershell
$oq = "$env:LOCALAPPDATA\CodexQuotaGuard\bin\orchestrator.exe"
& $oq ui
```

The dashboard provides:

- 5-hour and weekly quota cards;
- running/paused/review task counts;
- projects grouped by workspace;
- project create/edit/archive/delete;
- task search and filters by project/state;
- editable task objective, project assignment and notes;
- cooperative pause and cancel actions;
- `NEEDS_REVIEW` recovery actions;
- task state-event timeline;
- terminal task deletion.

Existing tasks with a workspace path are backfilled into projects the next time the daemon starts. New task registrations automatically create or reuse a project for their workspace.

The dashboard has no separate runtime dependency: its HTML/CSS/JS assets are embedded into the Go binary.

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
& $oq ui
& $oq status
& $oq list
& $oq doctor
& $oq version
```

`status` gives the practical overview: daemon state, dashboard URL, latest 5-hour/weekly quota, thresholds, relay state and task counts.

Starting `orchestrator daemon` while the configured daemon is already healthy is safe; it reports that the daemon is already running and exits successfully.

## Dashboard API

The embedded UI uses the daemon's local REST API:

```text
GET    /v1/dashboard
GET    /v1/projects
POST   /v1/projects
GET    /v1/projects/{id}
PATCH  /v1/projects/{id}
DELETE /v1/projects/{id}
GET    /v1/projects/{id}/tasks

GET    /v1/dashboard/tasks
GET    /v1/dashboard/tasks/{id}
PATCH  /v1/dashboard/tasks/{id}
DELETE /v1/dashboard/tasks/{id}
GET    /v1/dashboard/tasks/{id}/events
POST   /v1/dashboard/tasks/{id}/pause
POST   /v1/dashboard/tasks/{id}/cancel
POST   /v1/dashboard/tasks/{id}/retry
POST   /v1/dashboard/tasks/{id}/running
```

Task state is not freely PATCHable. Lifecycle changes still go through the existing state machine.

## Manual recovery

If a task becomes `NEEDS_REVIEW`, first inspect the destination Codex Desktop thread. You can resolve it from the dashboard or CLI:

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
  orchestrator/       daemon, dashboard launcher, quota doctor, status, recovery CLI
  desktop-companion/  MCP server launched by Codex Desktop
internal/
  codexquota/         quota-only app-server client
  daemon/             quota monitor + local control/dashboard API
  desktop/            Windows native Desktop delivery
  mcpserver/          MCP tools + background action delivery
  store/              SQLite persistence, projects/task metadata and action lifecycle
  domain/             task/project models and task states
  quota/              deterministic policy
  webui/              embedded local dashboard
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
