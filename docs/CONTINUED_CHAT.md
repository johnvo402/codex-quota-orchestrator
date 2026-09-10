# Continued chat task lifecycle

Codex Desktop keeps the same `threadId` while a user can send many separate turns in the same chat. The quota guard therefore treats the thread as the stable native-delivery destination, not as a one-time task execution.

When `desktop_task_register` reaches the daemon:

- a new thread is registered as `RUNNING`;
- repeated registration while the task is already `RUNNING` only refreshes metadata;
- a terminal task (`COMPLETED`, `FAILED`, or `CANCELLED`) is re-armed as `RUNNING` only when Codex supplies a different non-empty `turnId`;
- registering the same completed turn is idempotent and does not resurrect it;
- `PAUSE_REQUESTED`, `PAUSED_QUOTA`, `RESUME_QUEUED`, and `NEEDS_REVIEW` are not overwritten by a new register call.

Re-arming a terminal thread clears checkpoint/pending/test/quota fields from the previous execution and records a `terminal -> RUNNING` task event with reason `new Desktop turn registered after terminal task`.

This keeps the existing one-row-per-thread storage model and preserves the same thread for native resume. The task event timeline records the lifecycle boundary. A future history model can snapshot completed objectives separately without changing Desktop delivery identity.
