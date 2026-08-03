# Validation run status

| Suite | Environment | Current status | Latest attempt | Latest pass | Source/deployment | Run ID | Evidence | Active finding |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Headless lifecycle | local | PASS | 2026-08-03T15:26:22Z | 2026-08-03T15:26:22Z | clean `5d62241` / no deployment | `20260803T152616Z-051963` | `tmp/2026-08-03/rungrid-headless-e2e/3/` (ignored local evidence) | Mixed-service and tab-only workspaces passed against Process Compose 1.120.0 after log-level and executable-discovery validation changes. |
| Graphical Warp smoke | local macOS | BLOCKED | not run | none | local source / no deployment | none | none | Requires a controlled user-visible Warp session. |
| Production | production | NOT_APPLICABLE | not applicable | not applicable | non-deployed CLI | none | none | Rungrid has no production service environment. |
