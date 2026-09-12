# Codex Desktop Quota Guard

Desktop-first quota-aware orchestration and local task management for Codex.

Codex Desktop remains the owner and UI for real work. The guard monitors Codex quota, persists task/checkpoint state, cooperatively pauses work at safe boundaries, resumes through the native Desktop pipe, and provides a local dashboard with project queues.

## Current capabilities

- Separate 5-hour and weekly Codex quota windows with independent resume thresholds.
- Local Go daemon with SQLite persistence.
- Codex Desktop MCP companion captures the current Desktop `threadId`.
- Cooperative pause/checkpoint/resume flow.
- Native Windows Desktop `send_message_to_thread` delivery.
- Crash-safe action claiming and `NEEDS_REVIEW` recovery.
- Project/workspace grouping and sequential project task queues.
- Per-project queue modes: `AUTO`, `MANUAL`, and `PAUSED`.
- Atomic drag-and-drop ordering for queued project tasks.
- Embedded local dashboard with task/project management, Settings, and diagnostics.
- Shared persisted configuration for daemon, companion, and CLI.
- Graceful daemon restart from Settings or `orch restart`.
- Graceful daemon shutdown with `orch stop`.
- `orch` CLI installed on the current user's PATH.
- Inno Setup based Windows installer/uninstaller.
- Desktop-bound daemon lifecycle: the companion starts the daemon on demand and the daemon exits after the last active companion disconnects or its lease expires.

## v0.2.0 behavior change

The experimental **Desktop Stop** feature was removed in v0.2.0. Quota Guard no longer exposes `/stop.html`, no dashboard hard-stop action is created, and no Windows UI Automation is used to click Codex Desktop's Stop control.

Quota management remains cooperative: request pause, checkpoint at a safe boundary, finish the current turn, and resume later when quota allows. If an active turn must be interrupted immediately, use Codex Desktop's own Stop control directly.

When upgrading an older database, pending legacy Desktop Stop actions are cancelled. A legacy Stop action that had already entered delivery is preserved as `uncertain` and its nonterminal managed task is moved to `NEEDS_REVIEW` rather than guessing whether the old side effect occurred.

## Install on Windows

Prerequisites:

- Windows 10/11 x64
- Codex Desktop
- Codex CLI installed and logged in

Download the latest installer from GitHub Releases:

```text
CodexQuotaGuardSetup-vX.Y.Z.exe
```

Double-click it and complete the normal Windows setup wizard. No PowerShell installer or execution-policy change is required.

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

The installer handles normal application installation concerns such as files, User PATH, shortcuts, update compatibility, and uninstall registration. Codex-specific bootstrap is delegated to:

```powershell
orch setup
```

During installation that command registers the MCP companion, manages the Quota Guard block in `~/.codex/AGENTS.md`, and initializes the relay when needed.

After installation, fully quit and reopen Codex Desktop once, then open a new terminal and run:

```powershell
orch status
orch ui
```

## Daemon lifecycle

The daemon does not run permanently from the Windows Run key.

Normal lifecycle:

```text
Open Codex Desktop
   ↓
desktop-companion.exe starts
   ↓
companion starts orchestrator-daemon.exe if needed
   ↓
companion heartbeat keeps the daemon alive
   ↓
Full Quit Codex Desktop
   ↓
companion disconnects / lease expires
   ↓
orchestrator-daemon.exe gracefully exits
```

Multiple companion instances are supported. Closing one companion does not stop the daemon while another valid lease remains active.

If Codex or the companion crashes, the lease expires after the heartbeat grace period and the daemon shuts down automatically.

## Upgrade / repair

Download the newer installer and run it again:

```text
CodexQuotaGuardSetup-vX.Y.Z.exe
```

Before replacing installed binaries, the installer stops the currently installed daemon so `orchestrator-daemon.exe` is not left locked in Task Manager.

Current releases use the graceful CLI path:

```powershell
orch stop
```

For upgrades from releases that predate `orch stop`, the installer contains a narrowly scoped compatibility fallback that verifies the recorded daemon runtime identity before terminating that legacy process.

Runtime state is outside the installation directory and is preserved across upgrade/repair:

```text
~\.codex-desktop-quota-guard\
```

including SQLite task history, relay state, logs, and the shared `config.json` when it exists.

If Codex integration needs repair later:

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

or run:

```text
%LOCALAPPDATA%\CodexQuotaGuard\unins000.exe
```

The uninstaller stops the daemon before removing application files and runs:

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
```

## Shared configuration

With no explicit `--config` or `CDQG_CONFIG`, the daemon, Desktop companion, and `orch` CLI resolve the same optional file:

```text
~\.codex-desktop-quota-guard\config.json
```

Existing installs do not require this file. Built-in defaults remain active until Settings is saved for the first time.

The dashboard exposes:

```text
Settings
├─ hard / soft pause thresholds
├─ 5h resume threshold
├─ weekly resume threshold
├─ quota poll interval
├─ companion poll interval
├─ Codex request timeout
├─ global project queue auto-dispatch switch
└─ listen address
```

Default policy:

```text
hard threshold:    5%
soft threshold:   10%
5h resume:        20%
weekly resume:     5%
quota poll:       60s
companion poll:    5s
auto dispatch:   true
project queue:   AUTO
```

Hard and soft thresholds apply to both quota windows. Resume requires every available window to be above the soft pause boundary and to satisfy its own resume threshold.

A legacy config containing only:

```json
{
  "resumeThresholdPercent": 20
}
```

is still accepted. Newly saved configs use `fiveHourResumeThresholdPercent` and `weeklyResumeThresholdPercent`.

Settings are persisted through:

```text
GET /v1/settings
PUT /v1/settings
```

Writes use a temporary file and replace `config.json` only after the new content is fully written.

When saved settings differ from the running daemon, Settings exposes **Restart daemon**. The same operation is available from the CLI:

```powershell
orch restart
```

Restart is a graceful handoff: a hidden replacement waits for the predecessor health endpoint to disappear before binding with the new saved configuration.

If `companionPollSeconds` changes, fully reopen Codex Desktop as well so `desktop-companion.exe` reloads the shared configuration.

Explicit configuration remains supported:

```powershell
orch status --config C:\path\to\config.json
orch restart --config C:\path\to\config.json
orch stop --config C:\path\to\config.json
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

Switching a project to `PAUSED` does not interrupt work already running; it only prevents new queue items from starting. `AUTO` also requires the global `autoDispatch` setting to be enabled.

Queue safety rules:

- at most one queued item runs per project;
- only the first `QUEUED` item can be started manually;
- automatic advancement happens only for `AUTO` projects;
- the queue advances only after the previous managed task is `COMPLETED`;
- `FAILED`, `CANCELLED`, and `NEEDS_REVIEW` stop automatic advancement;
- low quota keeps future work queued, including manual Start requests;
- `PAUSED` blocks all new queue dispatch;
- queued positions are normalized after reorder, cancel, delete, and dispatch;
- stale/partial reorder requests are rejected;
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

Use `retry` only when you confirmed the previous continuation did not already arrive in the Desktop thread.

## Runtime logs and diagnostics

Structured JSON Lines logs are written under:

```text
~\.codex-desktop-quota-guard\logs\
├─ daemon.log
└─ companion.log
```

Useful commands:

```powershell
orch logs
orch logs --tail 200
orch logs --level error
orch logs --component daemon
orch logs --component companion
orch logs --follow
orch doctor
```

The diagnostics dashboard checks configuration, daemon/runtime metadata, SQLite and queue integrity, companion installation, Codex CLI, MCP registration, and quota-provider access. The native Desktop pipe remains a Desktop-process check and is reported as `UNKNOWN` from the standalone daemon/CLI diagnostics path.

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
  desktop/            native Desktop message delivery
  diagnostics/        doctor/dashboard health checks
  mcpserver/          MCP tools + action delivery
  observability/      structured runtime logging and log reader
  store/              SQLite persistence
  domain/             task/project/queue models
  quota/              policy
  webui/              embedded dashboard, queue, diagnostics, Settings
.github/workflows/
  ci.yml
  release.yml
  tag-release.yml
```

Legacy PowerShell scripts may remain in the source tree for development/migration support, but they are not the canonical release install/uninstall flow.

## Docs

- `docs/WINDOWS_SETUP.md` — Windows installation, upgrade, daemon lifecycle, configuration, and uninstall.
- `docs/TROUBLESHOOTING.md` — daemon, native delivery, and recovery diagnostics.
- `docs/DIAGNOSTICS.md` — doctor v2 and system diagnostics dashboard.
- `docs/LOGGING.md` — structured daemon/companion logs and `orch logs`.
- `docs/ARCHITECTURE.md` — Desktop-first architecture.

## Important limitations

Quota-driven pause remains cooperative at model/tool safe boundaries.

v0.2.0 does not provide a Desktop hard-stop control. Quota Guard does not claim a stable external interrupt capability for Desktop-owned active turns; stop an active turn directly in Codex Desktop when needed.
