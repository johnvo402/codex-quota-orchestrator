# Crash and restart recovery

Codex Quota Guard treats Desktop delivery as a crash-sensitive operation. The daemon never assumes that a message was delivered merely because an action was claimed.

## Action states

```text
pending -> delivering -> done
                     -> failed
                     -> uncertain
```

`pending` is safe to retry after a daemon restart because the action was never claimed. `delivering` is different: the previous process may have sent the message and crashed before recording the acknowledgement.

## Daemon restart behavior

At daemon startup, every action still in `delivering` is moved immediately to `uncertain`. There is no 60-second grace period on startup because a newly started daemon knows that those claims belonged to the previous process.

For managed resume actions:

```text
RESUME_QUEUED + delivering
    -> daemon restarts
    -> action uncertain
    -> task NEEDS_REVIEW
```

For project queue dispatches:

```text
DISPATCHING + delivering
    -> daemon restarts
    -> action uncertain
    -> project task NEEDS_REVIEW
```

The managed Desktop task is not reactivated automatically in the project-queue case. This prevents a blind duplicate dispatch.

Actions that are still `pending` remain pending and can be delivered normally after the daemon is healthy again.

## Runtime metadata

`daemon-runtime.json` is discovery metadata, not authority. The server writes it when serving begins and removes it whenever `Serve` exits, including unexpected listener/server failures. Cleanup remains PID-guarded so an old daemon cannot delete metadata written by its replacement during a graceful restart.

## Recovery after an uncertain outcome

Use Dashboard -> **Needs Review** and inspect the target Codex Desktop thread before choosing an action:

- **Retry delivery** only when the previous message did not arrive.
- **Mark running** when the previous message did arrive and work is already active.
- **Cancel** when the work should not continue.

The old uncertain action is retained as history. A retry creates a fresh action instead of changing the uncertain action back to pending.

## Periodic stale-delivery safety net

While the daemon remains alive, a delivery that stays in `delivering` for more than 60 seconds is also moved to `uncertain`. This protects against a companion/relay worker disappearing without restarting the daemon.
