# Migration from the CLI-owned MVP

The old prototype started user tasks through its own `codex app-server`. This Desktop-first branch intentionally stops doing that.

## Removed from the normal task path

- `orchestrator run "..."`
- owning user `thread/start` / `turn/start`
- external `thread/resume` for Desktop-owned tasks
- external `turn/interrupt` assumptions

## Kept

- quota reading through a short-lived app-server
- deterministic thresholds
- SQLite durability
- task state/history
- Windows Codex launcher handling for npm-installed `codex.cmd`

## Added

- `desktop-companion.exe` as a Codex MCP server
- automatic current Desktop `threadId` extraction from MCP request metadata
- cooperative `quota_check`, checkpoint and pause tools
- a Desktop-native message relay for auto-resume
- one-time relay executor bootstrap
- global AGENTS instructions installed in a marked block

## Why

A Desktop-open thread has a writer owned by Desktop. The new design leaves that writer untouched and sends resume messages through Desktop's own native task tool path instead of trying to resume the thread from a second app-server.
