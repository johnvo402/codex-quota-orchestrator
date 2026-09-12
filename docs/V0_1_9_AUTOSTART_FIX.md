# v0.1.9 daemon autostart fix

This patch hardens the Codex Desktop-bound daemon lifecycle after v0.1.8.

## Changes

- `Store.Open` eagerly applies the additive `actions.error_text` migration before startup recovery runs.
- Startup recovery failures are logged but no longer terminate the daemon before `/healthz` can come online.
- `desktop-companion.exe` starts its MCP stdio server immediately and performs daemon startup/retry in the lifecycle goroutine, so a slow database open/migration cannot delay the Codex MCP handshake.
- Auto-started daemon processes are waited in a background goroutine so an early process exit is recorded in `companion.log` instead of disappearing silently.

## Expected lifecycle

```text
Open Codex Desktop
  -> desktop-companion.exe starts
  -> MCP stdio handshake remains responsive
  -> lifecycle heartbeat checks daemon
  -> daemon is spawned on demand
  -> heartbeat registers the companion lease

Full Quit Codex Desktop
  -> companion disconnects or lease expires
  -> daemon exits gracefully
```
