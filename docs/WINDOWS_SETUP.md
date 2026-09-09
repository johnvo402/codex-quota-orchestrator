# Windows setup

## Prerequisites

- Go 1.23+
- Codex CLI installed and logged in with ChatGPT
- Codex Desktop
- PowerShell

Verify:

```powershell
go version
codex --version
```

## Install

From the project root:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-windows.ps1
```

The script:

- restores modules and runs tests/vet;
- builds `orchestrator.exe` and `desktop-companion.exe`;
- registers `desktop-quota-guard` through `codex mcp add`;
- adds a marked Quota Guard block to `~/.codex/AGENTS.md`;
- creates a relay executor thread unless `-SkipRelayInit` is passed.

Fully restart Codex Desktop after installation.

## Start the daemon

```powershell
.\bin\orchestrator.exe daemon
```

Leave it running. In another terminal:

```powershell
.\bin\orchestrator.exe doctor
```

`doctor` reports ChatGPT quota, relay configuration and whether native Desktop delivery can currently be discovered.

## Use Codex Desktop normally

Open a project and ask Codex to do substantial work as usual. The global instructions tell the agent to register the task and use the MCP quota tools at safe boundaries.

List tracked Desktop tasks:

```powershell
.\bin\orchestrator.exe list
```

## Expected pause/resume behavior

When quota becomes low, `quota_check` returns `action=pause`. The task checkpoints, marks itself `PAUSED_QUOTA`, and ends its turn. When quota is healthy again, the daemon queues a resume action. A companion process launched inside Desktop delivers a continuation prompt to the same thread through the Desktop native pipe.

## Important limitation

This version does not have a supported hard-interrupt API for arbitrary active Desktop turns. `PAUSE_REQUESTED` is cooperative. Do not rely on it as a safety boundary for destructive commands.

If `doctor` says native delivery is unavailable, pause/checkpoint still works, but automatic resume cannot be delivered until the native pipe is available to the companion.
