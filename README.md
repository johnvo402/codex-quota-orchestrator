# Codex Desktop Quota Guard

Desktop-first quota-aware orchestration for Codex.

This version keeps **Codex Desktop as the owner and UI for all real work**. It does not start user tasks in a separate CLI app-server.

## What works

- Codex Desktop tasks register through a local MCP companion.
- The MCP call receives the current Desktop `threadId` from Codex metadata.
- A local Go daemon reads ChatGPT/Codex quota through short-lived `codex app-server --stdio` sessions.
- SQLite persists managed Desktop tasks, checkpoints, quota and queued actions.
- At safe boundaries the model receives `continue` or `pause` from `quota_check`.
- Paused tasks are resumed by sending a continuation prompt back into the same Desktop thread through the native Desktop tools pipe when that pipe is available.
- Windows-first; no UI clicking or screen automation.

## Deliberate limitation

There is no claimed hard-stop implementation for arbitrary active Desktop turns in this release. The current native integration used here supports sending a message to a Desktop thread, while a stable external interrupt tool is not assumed. Pause is therefore cooperative and happens at model/tool safe boundaries.

## Quick start (Windows)

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-windows.ps1
.\bin\orchestrator.exe daemon
```

Then fully restart Codex Desktop and use it normally.

In a second terminal:

```powershell
.\bin\orchestrator.exe doctor
.\bin\orchestrator.exe list
```

See `docs/WINDOWS_SETUP.md` and `docs/ARCHITECTURE.md`.

## Project layout

```text
cmd/
  orchestrator/       daemon, quota doctor, relay bootstrap
  desktop-companion/  MCP server launched by Codex Desktop
internal/
  codexquota/         short-lived quota-only app-server client
  daemon/             quota monitor + local control API
  desktop/            Windows native Desktop delivery
  mcpserver/          MCP tools + background action delivery
  store/              SQLite persistence
  domain/             task states
  quota/              deterministic policy
scripts/
  install-windows.ps1
  uninstall-windows.ps1
```

## Default policy

- soft pause: 10% remaining
- hard warning: 5% remaining (still cooperative in Desktop mode)
- auto resume: 20% remaining
- quota poll: every 60 seconds

Edit `config.example.json` and use `--config` / `CDQG_CONFIG` for overrides.
