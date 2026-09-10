# Codex Desktop Stop

`Desktop Stop` interrupts the active Codex Desktop turn without sending a chat message.

## Why it does not use `send_message_to_thread`

A prompt such as "please stop" is cooperative and can arrive too late. The Stop feature instead operates the same visible Stop control that the user would press in Codex Desktop.

The upstream Codex app-server protocol has `turn/interrupt(threadId, turnId)`, but Codex Desktop does not currently expose its owning app-server control connection to this companion. Starting a second app-server for the same Desktop thread is intentionally avoided because it can conflict with Desktop's active writer.

## Windows flow

1. The dashboard queues a durable `stop` action containing the exact tracked `threadId` and expected `turnId`.
2. Before the action is claimed, SQLite verifies the task is still `RUNNING` and still tracks that exact turn.
3. The companion uses the existing `codex_app` native pipe to navigate Desktop to the target thread.
4. Immediately before clicking Stop, the companion calls native `read_thread` and verifies that the latest Desktop turn id still equals the queued expected turn id.
5. Windows UI Automation searches exactly one visible Codex window for exactly one allow-listed Stop button and invokes its `InvokePattern`.
6. Only after confirmed invocation does the durable action complete and the managed task become `CANCELLED`.

No prompt is sent to the target thread.

## Fail-closed rules

The companion refuses to click when the expected turn is missing, the Desktop thread has moved to a newer turn, native thread verification fails, multiple visible Codex windows make the target ambiguous, multiple matching Stop controls are found, or no supported Stop control can be found.

A failure before the UI Automation invocation leaves the managed task running and records the Stop action as failed. If invocation may already have happened but the local result is uncertain, the action becomes `uncertain` and the task moves to `NEEDS_REVIEW`; it is never clicked again automatically.

This prevents a stale queued Stop from accidentally interrupting newer work in the same chat.

## Project queue behavior

If the managed Desktop task corresponds to a running project queue item, a confirmed Stop also marks that queue item `CANCELLED`. An uncertain Stop moves running queue work to `NEEDS_REVIEW`.

## Platform scope

The visible Desktop Stop automation is Windows-only for now. Other platforms return an unsupported error rather than pretending the turn was stopped.

Desktop Stop follows Codex Desktop's own Stop semantics. It does not separately kill arbitrary background processes beyond whatever Codex Desktop itself stops.
