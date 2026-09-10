# Safe cancellation

Cancellation must not leave an old Desktop action available for delivery after the UI reports a task as cancelled.

## Managed task resume

```text
RESUME_QUEUED + pending resume
  -> action cancelled
  -> task CANCELLED
  -> later claim rejected

RESUME_QUEUED + delivering resume
  -> action uncertain
  -> task NEEDS_REVIEW
  -> cancellation returns conflict
  -> never pretend the Desktop side effect did not happen

RESUME_QUEUED + done resume
  -> cancellation rejected
  -> inspect the Desktop thread because the message was already delivered
```

Pending non-project Desktop notices for a managed thread are also invalidated when that task is cancelled.

## Project queue dispatch

```text
QUEUED
  -> CANCELLED

DISPATCHING + pending action
  -> action cancelled
  -> queue item CANCELLED

DISPATCHING + delivering action
  -> action uncertain
  -> queue item NEEDS_REVIEW
  -> cancellation returns conflict

RUNNING / already delivered
  -> cancellation rejected
  -> a future cooperative-stop flow is required to interrupt active Desktop work safely
```

The queue item and linked action are changed in one SQLite transaction. `ClaimAction` only accepts `pending`, so an action invalidated as `cancelled` cannot later be claimed by another companion process.

## Claim-vs-cancel race

Claim and cancellation are serialized by SQLite. Exactly one safe outcome wins:

```text
cancel wins first
  -> pending -> cancelled
  -> ClaimAction fails

claim wins first
  -> pending -> delivering
  -> cancel observes delivering
  -> uncertain + NEEDS_REVIEW
```

There is no path where the durable task is reported `CANCELLED` while a previously pending resume/project-dispatch action remains claimable.
