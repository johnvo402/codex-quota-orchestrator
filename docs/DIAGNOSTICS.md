# System diagnostics

The CLI and dashboard use the same diagnostics engine.

Run from a terminal:

```powershell
orch doctor
```

For machine-readable output:

```powershell
orch doctor --json
```

Open the dashboard and choose **Diagnostics**, or navigate to:

```text
http://127.0.0.1:47631/diagnostics.html
```

The diagnostics page runs the same checks through the loopback-only daemon API.

## Checks

The current report covers:

- shared configuration validation;
- daemon `/healthz` reachability;
- `daemon-runtime.json` validity and live runtime endpoint;
- SQLite readability;
- project queue ordering integrity (`QUEUED` positions must be `1..N`);
- installed `desktop-companion.exe` beside the orchestrator binary;
- Codex CLI discovery;
- `desktop-quota-guard` MCP registration;
- Codex ChatGPT account/rate-limit access;
- Desktop native tools pipe status.

The native tools pipe is reported as `UNKNOWN` from ordinary CLI/dashboard diagnostics because only a process inside the Codex Desktop process tree can prove that pipe. Run `desktop_guard_status` from a Codex Desktop task for that check.

## Statuses

- `PASS`: the check was completed and healthy.
- `WARN`: usable, but something is missing/stale or deserves attention.
- `FAIL`: a required check failed. `orch doctor` exits non-zero when any check fails.
- `UNKNOWN`: the current process cannot safely verify that check. `UNKNOWN` alone does not downgrade an otherwise healthy overall report.

## API guard

`GET /v1/diagnostics` is intended for the bundled same-origin dashboard. Because running diagnostics may start a short-lived Codex app-server, the endpoint requires:

```text
X-CDQG-Control: diagnostics
```

This prevents an unrelated website from triggering diagnostics through a simple cross-site localhost request.

## Privacy

The report intentionally does not expose the ChatGPT account email. It may contain local paths, process IDs, local ports, project/task counts, plan type, rate-limit data, and diagnostic error text. Review JSON output before sharing it publicly.
