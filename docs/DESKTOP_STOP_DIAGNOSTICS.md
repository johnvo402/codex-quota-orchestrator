# Desktop Stop diagnostics

Desktop Stop is tracked as a durable action. The dashboard follows the exact `actionId` returned when a Stop request is queued instead of inferring success from task state alone.

Action states:

- `pending`: the companion has not claimed the Stop yet. A prolonged pending state usually means the native Desktop probe or relay is unavailable.
- `delivering`: the companion claimed the action and is navigating/verifying/invoking the Stop control.
- `done`: Codex Desktop confirmed the exact tracked turn became `interrupted`; the managed task becomes `CANCELLED`.
- `failed`: Stop failed before any Stop side effect was attempted. The managed task stays `RUNNING` and the action stores the concrete error for safe retry/debugging.
- `uncertain`: a Stop side effect may have been attempted but interruption was not confirmed. The managed task becomes `NEEDS_REVIEW`; do not blindly retry.
- `cancelled`: the tracked task or turn changed before the action could be safely claimed.

The Stop page waits up to 60 seconds because a valid flow can include the companion polling interval, a native Desktop probe, UI automation, and backend verification. It reports the durable action state and recorded error instead of showing an early generic timeout.
