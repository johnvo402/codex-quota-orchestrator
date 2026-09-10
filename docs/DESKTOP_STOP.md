# Codex Desktop Stop

`Desktop Stop` requests interruption of the exact active Codex Desktop turn without sending a chat message.

## Why it does not use `send_message_to_thread`

A prompt such as "please stop" is cooperative and can arrive too late. The Stop feature instead operates Codex Desktop's visible Stop control.

The upstream Codex app-server protocol has `turn/interrupt(threadId, turnId)`, but Codex Desktop does not expose its owning app-server control connection to this companion. Starting a second app-server for a Desktop-owned thread is intentionally avoided because it can compete for the same thread/state writer.

## Windows flow

1. The dashboard queues a durable `stop` action containing the exact tracked `threadId` and expected `turnId`.
2. Before the action is claimed, SQLite verifies the managed task is still `RUNNING` and still tracks that exact turn.
3. The companion uses the existing `codex_app` native pipe to navigate Desktop to the target thread.
4. Immediately before any Stop side effect, native `read_thread` must report the exact expected turn and `status=inProgress`.
5. Windows UI Automation finds exactly one visible Codex window and exactly one allow-listed Stop button.
6. The first attempt uses the button's `InvokePattern`.
7. The companion reads the thread back. Success is accepted only when that same turn reports `status=interrupted`.
8. If the exact same turn is still `inProgress`, the companion performs one guarded fallback: it re-verifies the turn, foregrounds the unique Codex window, reacquires the unique Stop button, gets its UI Automation clickable point, performs one physical click, and restores the cursor position.
9. The thread is read back again. Only `status=interrupted` completes the durable Stop action and changes the managed task to `CANCELLED`.

No prompt is sent to the target thread.

## Why post-verification matters

A successful Windows UI Automation call only proves that an accessibility action was issued. It does not prove that Codex's backend turn actually stopped.

Some Codex Desktop Windows builds have exhibited cases where the visible Stop control itself does not interrupt the active backend turn. Because of that, Quota Guard never treats `InvokePattern.Invoke()` or a physical click alone as success.

If the Stop control was attempted but the same turn remains `inProgress`, or the read-back result is otherwise ambiguous, the action becomes `uncertain` and the managed task becomes `NEEDS_REVIEW`.

## Fail-closed rules

The companion refuses to attempt Stop when:

- the expected turn is missing;
- the Desktop thread has moved to a newer turn;
- native `read_thread` cannot verify the target;
- the target turn is not `inProgress`;
- multiple visible Codex windows make the target ambiguous;
- multiple matching Stop controls are found;
- no supported Stop control can be found;
- the guarded physical-click fallback cannot foreground/reacquire the exact target safely.

A failure before any UI action leaves the managed task `RUNNING` and records the Stop action as failed.

Once any Stop side effect has been attempted, an unverified result is never blindly retried. It becomes `uncertain` / `NEEDS_REVIEW` so a human can inspect the Desktop thread first.

This prevents a stale queued Stop from accidentally interrupting newer work in the same chat.

## Dashboard feedback

The Desktop Stop page no longer treats `202 Accepted` as completion. After queuing a Stop it waits for the managed task result:

- `CANCELLED` means the exact backend turn was read back as `interrupted`;
- `NEEDS_REVIEW` means a Stop side effect was attempted but interruption was not confirmed;
- remaining `RUNNING` means no successful Stop was recorded.

For investigation, inspect:

```powershell
orch logs --component companion --tail 200
```

## Project queue behavior

If the managed Desktop task corresponds to a running project queue item, a verified Stop also marks that queue item `CANCELLED`. An uncertain Stop moves running queue work to `NEEDS_REVIEW`.

## Platform scope

Visible Desktop Stop automation is Windows-only for now. Other platforms return an unsupported error rather than pretending the turn was stopped.

Desktop Stop follows Codex Desktop's own interruption semantics. It does not separately kill arbitrary Codex worker/terminal processes, and it cannot guarantee interruption when the current Codex Desktop build's own Stop path is broken. In that case the guard reports uncertainty instead of false success.
