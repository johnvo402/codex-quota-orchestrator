# Desktop-first architecture

## Goal

Keep Codex Desktop as the human-facing owner of threads. The guard must not resume a Desktop-owned thread through a second app-server, because that can contend with Desktop's writer ownership.

## Components

```text
Codex Desktop
  └─ launches desktop-companion.exe as MCP
       ├─ MCP tools receive current threadId metadata
       ├─ calls local daemon over HTTP
       └─ when available, uses Desktop native tools pipe
              └─ codex_app.send_message_to_thread(targetThreadId, prompt)

orchestrator.exe daemon
  ├─ short-lived quota-only codex app-server reads
  ├─ SQLite state.db
  ├─ quota policy
  └─ pending Desktop delivery actions
```

## Why cooperative pause

The current Desktop native task-tool path used by this project has a known send-to-thread operation, but no stable documented external hard-interrupt operation. Therefore v0.2 does not pretend it can forcibly stop an arbitrary active Desktop generation.

Instead, installed global instructions make managed Desktop tasks:

1. register themselves through MCP;
2. check quota at substantial phase boundaries;
3. checkpoint and end the current turn when the guard says pause;
4. get a new user message in the same Desktop thread after quota recovers.

This keeps the thread visible and owned by Desktop throughout.

## Native relay

On Windows, the companion feature-detects `CODEX_APP_TOOLS_PIPE_PATH` (or `CDQG_DESKTOP_NATIVE_PIPE`) and uses a 4-byte little-endian length prefix followed by UTF-8 JSON-RPC. The dispatch is deliberately isolated in `internal/desktop/` because this is an internal Desktop protocol and may change.

A persistent relay executor thread is created once by `orchestrator relay-init`. The short-lived app-server used to create it exits immediately after a tiny first turn so it does not keep the thread writer lock.

## State flow

```text
RUNNING
  -> PAUSE_REQUESTED       quota <= soft threshold
  -> PAUSED_QUOTA          agent reaches safe boundary and calls task_mark_paused
  -> RESUME_QUEUED         quota >= resume threshold
  -> RUNNING               native resume message acknowledged
  -> COMPLETED             agent calls task_complete
```

If the task ignores the MCP policy, the daemon cannot guarantee a hard stop in this version.
