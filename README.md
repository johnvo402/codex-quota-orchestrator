# Codex Desktop Quota Guard

Desktop-first quota-aware orchestration and local task management for Codex.

Codex Desktop remains the owner and UI for real work. The guard monitors Codex quota, persists task/checkpoint state, cooperatively pauses work at safe boundaries, resumes through the native Desktop pipe, and provides a local dashboard with project queues.

## Current capabilities

- Separate 5-hour and weekly Codex quota windows.
- Local Go daemon with SQLite persistence.
- Codex Desktop MCP companion captures the current Desktop `threadId`.
- Cooperative pause/checkpoint/resume flow.
- Native Windows Desktop `send_message_to_thread` delivery.
- Crash-safe action claiming and `NEEDS_REVIEW` recovery.
- Project/workspace grouping and sequential project task queues.
- Per-project queue modes: `AUTO`, `MANUAL`, and `PAUSED`.
- Embedded local dashboard with task/project management and Settings.
- Shared persisted configuration for daemon, companion, and CLI.
- Graceful daemon restart from Settings or `orch restart`.
- `orch` CLI installed on the current user's PATH.
- Inno Setup based Windows installer/uninstaller.
- Hidden Windows background daemon with per-user autostart.

## Install on Windows

Prerequisites:

- Windows 10/11 x64
- Codex Desktop
- Codex CLI installed and logged in

Download the latest installer directly from GitHub Releases:

```text
CodexQuotaGuardSetup-vX.Y.Z.exe
```

Double-click it and complete the normal Windows setup wizard. No PowerShell script or execution-policy change is required.

Setup installs under:

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

The installer itself only handles normal application installation concerns: files, User PATH, Windows autostart, shortcuts, and uninstall registration. Codex-specific bootstrap is delegated to:

```powershell
orch setup
```

During installation Inno Setup runs that command to register the MCP companion, manage the Quota Guard block in `~/.codex/AGENTS.md`, and initialize the relay when needed.

After installation, fully quit and reopen Codex Desktop once, then open a new terminal and run:

```powershell
orch status
orch ui
```

## Upgrade / repair

Download the newer `CodexQuotaGuardSetup-vX.Y.Z.exe` and run it again.

Inno Setup uses the Windows Restart Manager for files that are currently in use rather than force-killing processes itself. Runtime state remains outside the install directory and is preserved:

```text
~\.codex-desktop-quota-guard\
```

including SQLite task history, relay state, and the shared `config.json` when it exists.

If Codex integration ever needs to be repaired manually:

```powershell
orch setup
```

## Uninstall

Use:

```text
Settings
→ Apps
→ Installed apps
→ Codex Desktop Quota Guard
→ Uninstall
```

or run the Inno Setup uninstaller inside:

```text
%LOCALAPPDATA%\CodexQuotaGuard\unins000.exe
```

Before removing application files, the uninstaller runs:

```powershell
orch teardown
```

which removes the `desktop-quota-guard` MCP registration and the managed Quota Guard block from `AGENTS.md`.

Runtime state under `~\.codex-desktop-quota-guard\` is intentionally preserved so reinstall/upgrade does not destroy task history or settings.

## Everyday commands

```powershell
orch setup
orch teardown
orch config show
orch config path
orch config validate
orch restart
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

Task Queue:

```text
http://127.0.0.1:47631/queue.html
```

Settings:

```text
http://127.0.0.1:47631/settings.html
```

## Shared configuration

With no explicit `--config` or `CDQG_CONFIG`, the daemon, Desktop companion, and `orch` CLI all resolve the same optional file:

```text
~\.codex-desktop-quota-guard\config.json
```

Existing installs do not need this file. Defaults remain active until Settings is saved for the first time.

The dashboard exposes:

```text
Settings
├─ hard / soft / resume thresholds
├─ quota poll interval
├─ companion poll interval
├─ Codex request timeout
├─ global project queue auto-dispatch switch
└─ listen address
```

Settings are validated and persisted through:

```text
GET /v1/settings
PUT /v1/settings
```

Writes use a temporary file and replace `config.json` only after the new content is fully written. When persisted settings differ from the running daemon, Settings shows a **Restart daemon** button. The same operation is available from the CLI:

```powershell
orch restart
```

Restart is a graceful handoff: the running daemon starts a hidden replacement process, returns the accepted control request, shuts down its HTTP server, and the replacement waits for the predecessor health endpoint to disappear before binding with the saved configuration. No `taskkill` is used.

If the companion poll setting changes, fully reopen Codex Desktop as well so `desktop-companion.exe` reloads the shared configuration.

Explicit configuration remains supported:

```powershell
orch status --config C:\path\to\config.json
orch restart --config C:\path\to\config.json
orch config --config C:\path\to\config.json show
```

`CDQG_CONFIG`, `CDQG_DATA_DIR`, and `CDQG_CODEX_COMMAND` remain available for advanced/development overrides.

## Sequential project task queue

Each project has an independent sequential queue and its own queue mode:

```text
AUTO    Automatically dispatch the next queued item after successful completion.
MANUAL  Keep queued work waiting until Start now is pressed.
PAUSED  Block all new queue dispatch for the project.
```

Switching a project to `PAUSED` does not interrupt work that is already running; it only prevents new queue items from starting. `AUTO` also requires the global `autoDispatch` setting to be enabled. Manual Start bypasses automatic-dispatch policy, but it does not bypass quota, ordering, active-task, or crash-safety checks.

Example:

```text
Project: DotRadar       Mode: MANUAL

COMPLETED Implement SARIF output
QUEUED    Add GitHub Actions integration   [Start now]
QUEUED    Improve README examples
```

Queue mode is persisted separately per project and is available through:

```text
GET   /v1/projects/{projectId}/queue-mode
PATCH /v1/projects/{projectId}/queue-mode
POST  /v1/project-tasks/{taskId}/start
```

Safety rules:

- at most one queued item runs per project;
- only the first `QUEUED` item can be started manually;
- automatic advancement happens only for `AUTO` projects;
- the queue advances only after the previous managed task is `COMPLETED`;
- `FAILED`, `CANCELLED`, and `NEEDS_REVIEW` stop automatic advancement;
- low quota keeps future work queued, including manual Start requests;
- `PAUSED` blocks all new queue dispatch;
- uncertain delivery becomes `NEEDS_REVIEW` instead of being blindly retried.

Lifecycle:

```text
QUEUED -> DISPATCHING -> RUNNING -> COMPLETED
                         |
                         +-> NEEDS_REVIEW
```

## Manual recovery

If a managed task becomes `NEEDS_REVIEW`, inspect the destination Desktop thread first, then use:

```powershell
orch recover --thread <threadId> --resolution retry
orch recover --thread <threadId> --resolution running
orch recover --thread <threadId> --resolution cancel
```

Use `retry` only when you confirmed the previous continuation did not arrive.

## Default policy

```text
hard threshold:  5%
soft threshold: 10%
resume:          20%
quota poll:      60s
companion poll:   5s
auto dispatch:   true
project queue:   AUTO
```

## Project layout

```text
cmd/
  orchestrator/       daemon + CLI + Codex bootstrap/config commands
  desktop-companion/  MCP server launched by Codex Desktop
installer/
  CodexQuotaGuard.iss Inno Setup definition
internal/
  codexquota/         short-lived Codex app-server quota client
  config/             shared config resolution, validation, persistence
  daemon/             monitor + API + queue scheduler
  desktop/            native Desktop delivery
  mcpserver/          MCP tools + action delivery
  store/              SQLite persistence
  domain/             task/project/queue models
  quota/              policy
  webui/              embedded dashboard, queue, and Settings UI
.github/workflows/
  ci.yml
  release.yml
```

Legacy PowerShell scripts may remain in the source tree for development/migration support, but they are not the canonical release install/uninstall flow.

## Docs

- `docs/WINDOWS_SETUP.md` — Windows installation, upgrade, background startup, configuration, and uninstall.
- `docs/TROUBLESHOOTING.md` — daemon, native delivery, and recovery diagnostics.
- `docs/ARCHITECTURE.md` — Desktop-first architecture.

## Important limitation

Pause is cooperative at model/tool safe boundaries. The project does not claim a stable hard-interrupt API for arbitrary active Codex Desktop turns.
