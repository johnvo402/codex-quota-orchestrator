# Runtime logging

Codex Desktop Quota Guard persists structured runtime logs for the two background components that are hardest to inspect directly:

```text
~/.codex-desktop-quota-guard/logs/
├─ daemon.log
├─ daemon.log.1
├─ daemon.log.2
├─ daemon.log.3
├─ companion.log
├─ companion.log.1
├─ companion.log.2
└─ companion.log.3
```

Each active log file is JSON Lines written through Go `log/slog`. A file rotates at roughly 5 MB and up to three backups are retained. Runtime logs stay under the data directory and are not removed by a normal application upgrade/uninstall.

## CLI

Show the newest 200 INFO-and-higher records from both components:

```powershell
orch logs
```

Follow new records until Ctrl+C:

```powershell
orch logs --follow
```

Useful filters:

```powershell
orch logs --tail 50
orch logs --level warn
orch logs --level error
orch logs --component daemon
orch logs --component companion
```

Print the original JSON records for scripts or diagnostics tooling:

```powershell
orch logs --json
orch logs --follow --json
```

`--tail 0` reads all retained records. `--level` accepts `all`, `debug`, `info`, `warn`, and `error`. `--component` accepts `all`, `daemon`, and `companion`.

## Important events

The daemon records events such as:

- daemon start/stop/server failure;
- quota samples and quota refresh failures;
- cooperative pause requests;
- restoration when quota recovers before a pause finishes;
- Desktop resume actions being queued;
- project task dispatches;
- stale/uncertain delivery recovery;
- store and queue reconciliation failures.

The companion records its own start/stop state and whether it could auto-start/reach the daemon. Existing MCP/native-delivery log messages also flow through the same companion logger.

## Privacy

Logs are local files. They can contain operational identifiers such as Desktop thread IDs, project/task IDs, local paths, quota percentages, and error messages. Do not publish a full log bundle without reviewing it first.
