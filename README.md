# Codex Desktop Quota Guard

Desktop-first quota-aware orchestration and local task management for Codex. Codex Desktop remains the owner and UI for real work; the guard monitors quota, persists tasks and checkpoints, cooperatively pauses at safe boundaries, resumes through Codex Desktop's native tools pipe, and manages local project queues.

## Highlights

- Separate 5-hour and weekly quota windows with independent resume thresholds.
- Local Go daemon backed by SQLite.
- Codex Desktop MCP companion captures the current Desktop `threadId` and starts/stops the daemon with Codex Desktop.
- Cooperative quota pause, checkpoint, and resume.
- Native Windows Desktop delivery through `codex_app.send_message_to_thread`.
- Crash-safe action claiming with `NEEDS_REVIEW` for uncertain external side effects.
- Projects/workspaces with sequential queues and `AUTO`, `MANUAL`, and `PAUSED` queue modes.
- Queue self-healing: if a managed Desktop task is already `COMPLETED` but its queue row was left `RUNNING` by an older build/crash window, reconciliation repairs the stale row and allows the next queued task to advance.
- Atomic drag-and-drop queue ordering.
- Embedded dashboard, settings, diagnostics, logs, and Desktop Stop page.
- Windows installer/uninstaller and `orch` CLI on PATH.

## Install on Windows

Install the latest `CodexQuotaGuardSetup-<version>.exe` from GitHub Releases.

The default install directory is:

```text
%LOCALAPPDATA%\CodexQuotaGuard\
```

Runtime data is stored under:

```text
%USERPROFILE%\.codex-desktop-quota-guard\
```

After install, fully quit and reopen Codex Desktop so the MCP companion starts with the current Desktop session.

Useful commands:

```powershell
orch version
orch doctor
orch status
orch ui
orch logs --follow
```

## Runtime lifecycle

The daemon is Desktop-bound by default:

```text
Codex Desktop opens
  -> desktop-companion.exe starts
  -> companion ensures orchestrator-daemon.exe is running
  -> companion heartbeat keeps the daemon alive

Codex Desktop fully quits
  -> companion disconnects
  -> daemon exits after the companion lease expires
```

This avoids leaving `orchestrator-daemon.exe` running indefinitely after a full Codex Desktop quit and makes in-place upgrades reliable. The installer also asks the existing daemon to stop before replacing binaries.

## Dashboard

Default local dashboard:

```text
http://127.0.0.1:47631/
```

Pages:

- `/` — overview and managed Desktop tasks
- `/queue.html` — project queues
- `/stop.html` — guarded Desktop Stop for active managed turns
- `/settings.html` — shared runtime settings
- `/diagnostics.html` — system diagnostics

You can also run:

```powershell
orch ui
```

## CLI

```text
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

Recovery for uncertain managed tasks:

```powershell
orch recover --thread <threadId> --resolution retry
orch recover --thread <threadId> --resolution running
orch recover --thread <threadId> --resolution cancel
```

## Quota policy

Default settings:

```text
hard threshold:       5%
soft threshold:       10%
5-hour resume:        20%
weekly resume:        5%
quota poll:           60s
companion poll:       5s
autoDispatch:         true
project queue mode:   AUTO
```

The orchestrator never bypasses or spoofs Codex quota. It only decides whether local managed work should continue, pause, or resume based on the quota reported by Codex.

## Managed task lifecycle

```text
RUNNING
  -> PAUSE_REQUESTED
  -> PAUSED_QUOTA
  -> RESUME_QUEUED
  -> RUNNING
  -> COMPLETED
```

`NEEDS_REVIEW` is the fail-closed state for uncertain Desktop side effects.

The semantic managed-task state is deliberately separate from durable action delivery state.

## Durable Desktop actions

External Desktop side effects use a crash-safe action state machine:

```text
pending
  -> delivering
  -> done
     failed
     uncertain
```

`pending` is retry-safe. If a process dies while an action is `delivering`, the result is not guessed; recovery marks it `uncertain`, and the affected task moves to `NEEDS_REVIEW` when appropriate.

## Project queues

A project queue item follows:

```text
QUEUED
  -> DISPATCHING
  -> RUNNING
  -> COMPLETED
```

`NEEDS_REVIEW` is a side path when delivery may have happened but cannot be proven.

Queue safety rules:

- Only the first queued item can start manually.
- At most one active item is allowed per project.
- `AUTO` advances automatically when global `autoDispatch` is enabled.
- `MANUAL` waits for an explicit Start action.
- `PAUSED` blocks dispatch.
- The queue advances only after successful completion.
- `FAILED`, `CANCELLED`, and `NEEDS_REVIEW` do not silently advance the queue.
- Low quota blocks future starts.
- Stale queue ordering updates are rejected.
- A stale queue row left `RUNNING` while its linked managed Desktop task is already `COMPLETED` is repaired during blocker reconciliation so it cannot deadlock the project forever.

## Desktop Stop

Desktop Stop is a narrow Windows-only safety feature for an active managed Codex Desktop turn.

The flow is:

```text
Dashboard Stop
  -> durable stop action(threadId, expectedTurnId)
  -> exact task/turn validation in SQLite
  -> navigate Codex Desktop to the target thread
  -> read the latest Desktop turn through the native pipe
  -> verify latest turnId == expectedTurnId and status == inProgress
  -> invoke the unique visible Stop control
  -> verify the backend turn becomes interrupted
```

On Chromium/Electron builds where accessibility `InvokePattern` reports success but does not actually interrupt the turn, the Windows implementation may use one guarded physical-click fallback. It still fails closed: it requires one visible Codex window, one allow-listed Stop button, an exact expected turn, a valid clickable point, and a fresh native turn check.

The Stop page follows the exact durable `actionId` returned by the daemon. It distinguishes `pending`, `delivering`, `done`, `failed`, `uncertain`, and `cancelled` instead of inferring the outcome only from task state. This makes failures such as native-pipe problems, stale turns, or a missing Stop button visible directly in the UI.

If Stop may have been invoked but interruption cannot be confirmed, the action becomes `uncertain` and the managed task moves to `NEEDS_REVIEW`. The guard never blindly clicks Stop again.

## Desktop-first architecture

The current design intentionally does not run user work through a second orchestrator-owned Codex app-server.

```text
Codex Desktop
  -> desktop-companion.exe (MCP)
  -> local daemon HTTP API
  -> SQLite durable state

Desktop-native operations
  -> Codex Desktop native tools pipe
  -> codex_app tools
```

Codex Desktop remains the owner of human-facing threads and turns. The guard observes and coordinates them instead of becoming a second writer.

Quota reads use a short-lived Codex app-server client only for account/rate-limit information.

## Native Desktop delivery

On Windows, the companion discovers the Codex Desktop tools pipe from one of:

```text
CODEX_APP_TOOLS_PIPE_PATH
CDQG_DESKTOP_NATIVE_PIPE
parent Codex app-server command line
```

Native requests use the persistent relay executor thread and can call tools such as:

```text
codex_app.list_threads
codex_app.read_thread
codex_app.navigate_to_codex_page
codex_app.send_message_to_thread
```

The relay executor is bootstrap infrastructure; it is not the target user thread.

## Codex CLI resolution on Windows

The guard does not trust a `codex` executable found only through the current working directory. On Windows, `orch doctor`, setup, and quota sampling resolve Codex from an absolute configured path or absolute PATH entries and support the usual executable/script extensions such as `.exe`, `.cmd`, and `.bat`.

This avoids Go's `exec.ErrDot` behavior and prevents a local `codex.exe`/`codex.cmd` in the working directory from shadowing the real Codex CLI.

## Diagnostics

Run:

```powershell
orch doctor
```

Expected healthy checks include the Codex CLI, local data directory, SQLite state, quota provider, daemon/runtime discovery, and Desktop integration where available.

For live logs:

```powershell
orch logs --follow
```

The most useful files under the runtime data directory are typically:

```text
daemon.log
companion.log
state.db
config.json
relay.json
```

## Configuration

Default config path:

```text
%USERPROFILE%\.codex-desktop-quota-guard\config.json
```

Inspect it with:

```powershell
orch config show
orch config path
orch config validate
```

See `config.example.json` for the supported fields.

## Development

Requirements:

- Go 1.23+
- Windows for native Desktop Stop and installer validation
- Inno Setup 6 for local installer builds

Run tests:

```bash
go test ./...
go vet ./...
```

Windows CI additionally builds:

```text
orchestrator.exe
orchestrator-daemon.exe
desktop-companion.exe
CodexQuotaGuardSetup.exe
```

## Safety principles

- Do not bypass Codex quota.
- Do not guess whether an external Desktop side effect happened.
- Claim durable actions before delivery.
- Never blindly retry an uncertain Stop or send.
- Verify exact thread/turn identity before destructive Desktop actions.
- Keep Codex Desktop as the authoritative owner of user work.
- Prefer `NEEDS_REVIEW` over an unsafe automatic retry.

## More documentation

See the `docs/` directory for architecture, Windows setup, diagnostics, logging, recovery, crash recovery, safe cancellation, continued-chat behavior, and Desktop Stop details.
