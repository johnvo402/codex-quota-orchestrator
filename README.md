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
- Embedded local dashboard with task/project management.
- `orch` CLI installed on the current user's PATH.
- Native Windows EXE installer/uninstaller.
- Hidden Windows background daemon with per-user autostart.

## Install on Windows

Prerequisites:

- Windows 10/11 x64
- Codex Desktop
- Codex CLI installed and logged in

Download the latest Windows x64 ZIP from GitHub Releases, extract it, then double-click:

```text
CodexQuotaGuardSetup.exe
```

No PowerShell execution-policy change and no `.ps1` installer are required.

Setup installs stable files under:

```text
%LOCALAPPDATA%\CodexQuotaGuard\
├─ Uninstall.exe
└─ bin\
   ├─ orchestrator.exe
   ├─ orchestrator-daemon.exe
   ├─ orch.exe
   └─ desktop-companion.exe
```

It also:

- adds `%LOCALAPPDATA%\CodexQuotaGuard\bin` to the current User PATH;
- registers `desktop-quota-guard` through Codex CLI;
- installs the managed Quota Guard block in `~/.codex/AGENTS.md`;
- initializes the relay if needed;
- registers per-user Windows autostart;
- registers **Codex Desktop Quota Guard** in Windows Installed Apps;
- starts the daemon in the background with no console window.

After installation, fully quit and reopen Codex Desktop once, then open a new terminal and run:

```powershell
orch status
orch ui
```

## Upgrade / repair

Download and extract the newer release, then run its `CodexQuotaGuardSetup.exe` again.

The native setup behaves as install-or-upgrade and preserves:

```text
~\.codex-desktop-quota-guard\
```

including SQLite task history and relay state.

Before modifying an existing install, setup checks whether Codex Desktop is still using `desktop-companion.exe`. If it is, setup asks you to fully quit Codex Desktop and retry instead of leaving the installation half-upgraded.

## Uninstall

Use either:

```text
Settings
→ Apps
→ Installed apps
→ Codex Desktop Quota Guard
→ Uninstall
```

or double-click:

```text
%LOCALAPPDATA%\CodexQuotaGuard\Uninstall.exe
```

Interactive uninstall asks whether to also delete runtime state.

Command-line uninstall:

```powershell
& "$env:LOCALAPPDATA\CodexQuotaGuard\Uninstall.exe" uninstall --silent
```

Remove local state too:

```powershell
& "$env:LOCALAPPDATA\CodexQuotaGuard\Uninstall.exe" uninstall --silent --purge-data
```

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

Task Queue:

```text
http://127.0.0.1:47631/queue.html
```

## Sequential project task queue

Each project has an independent FIFO-style queue.

Example:

```text
Project: DotRadar

RUNNING  Implement SARIF output
QUEUED   Add GitHub Actions integration
QUEUED   Improve README examples
```

When the current task calls `task_complete`, the daemon checks quota and project state. If quota is healthy, it dispatches the next queued task through the same crash-safe native delivery pipeline.

Safety rules:

- at most one queued item runs per project;
- the queue advances only after `COMPLETED`;
- `FAILED`, `CANCELLED`, and `NEEDS_REVIEW` stop automatic advancement;
- low quota keeps future work queued;
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
```

Manual commands support `--config`, and background configuration can also use supported `CDQG_*` environment variables.

## Project layout

```text
cmd/
  orchestrator/       daemon + CLI
  desktop-companion/  MCP server launched by Codex Desktop
  setup/              native Windows installer/uninstaller
internal/
  codexquota/         short-lived Codex app-server quota client
  daemon/             monitor + API + queue scheduler
  desktop/            native Desktop delivery
  mcpserver/          MCP tools + action delivery
  store/              SQLite persistence
  domain/             task/project/queue models
  quota/              policy
  webui/              embedded dashboard
.github/workflows/
  ci.yml
  release.yml
```

Legacy PowerShell scripts may remain in the source tree for development/migration support, but they are not the canonical release install/uninstall flow.

## Docs

- `docs/WINDOWS_SETUP.md` — Windows installation, upgrade, background startup, and uninstall.
- `docs/TROUBLESHOOTING.md` — daemon, native delivery, and recovery diagnostics.
- `docs/ARCHITECTURE.md` — Desktop-first architecture.

## Important limitation

Pause is cooperative at model/tool safe boundaries. The project does not claim a stable hard-interrupt API for arbitrary active Codex Desktop turns.
