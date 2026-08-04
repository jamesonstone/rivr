# Validation run status

| Suite | Environment | Current status | Latest attempt | Latest pass | Source/deployment | Run ID | Evidence | Active finding |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Headless lifecycle | local | PASS | 2026-08-04T12:01:31Z | 2026-08-04T12:01:31Z | clean `534cc8c` / no deployment | `20260804T120123Z-084621` | `tmp/2026-08-04/rungrid-headless-e2e/1/` (ignored local evidence) | Mixed-service and tab-only workspaces passed against Process Compose 1.120.0 after the agent-instruction command was added. |
| Graphical Warp smoke | local macOS | BLOCKED | not run | none | local source / no deployment | none | none | Requires a controlled user-visible Warp session. |
| Production | production | NOT_APPLICABLE | not applicable | not applicable | non-deployed CLI | none | none | Rungrid has no production service environment. |
