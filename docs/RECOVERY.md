# NEEDS_REVIEW recovery

`NEEDS_REVIEW` is a safety state. It means Codex Quota Guard cannot prove whether a Desktop delivery arrived, so it will not blindly send the same work again.

Open the dashboard and choose **Needs Review**. The recovery view shows the affected project/task, destination Desktop thread, latest delivery action, action status, and the recorded recovery reason.

## Recovery choices

### Retry delivery

Use this only after opening the destination Codex Desktop thread and confirming that the uncertain message did **not** arrive.

For a managed resume, the task returns to `PAUSED_QUOTA`; the daemon queues a new continuation when quota is resumable.

For a project queue dispatch, a fresh action is created immediately only when quota and queue safety checks allow it. The old action remains `uncertain` as history.

### Mark running

Use this when the uncertain message **did** arrive and Codex Desktop is already executing the work.

No new Desktop message is sent. For a project queue item, the queue item and its managed Desktop task are activated together in one transaction.

### Cancel

Use this when the work should not continue. No new Desktop delivery is created.

## Safety rules

- uncertain actions are never blindly retried;
- retry always creates a new action rather than rewriting uncertain history;
- project queue retry still obeys quota thresholds and `PAUSED` queue mode;
- only one project queue item may be active at a time;
- `Mark running` never sends a Desktop message;
- recovery actions are rejected when the item is no longer `NEEDS_REVIEW`.

The project queue page exposes the same recovery controls for queue-backed `NEEDS_REVIEW` items.
