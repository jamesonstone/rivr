# Validation run status

| Suite | Environment | Current status | Latest attempt | Latest pass | Source/deployment | Run ID | Evidence | Active finding |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Headless lifecycle | local | PASS | 2026-08-03T14:42:12Z | 2026-08-03T14:42:12Z | clean `fb05076` / no deployment | `20260803T144206Z-012153` | `tmp/2026-08-03/rungrid-headless-e2e/1/` (ignored local evidence) | Mixed-service and tab-only workspaces passed against Process Compose 1.120.0. |
| Graphical Warp smoke | local macOS | BLOCKED | not run | none | local source / no deployment | none | none | Requires a controlled user-visible Warp session. |
| Production | production | NOT_APPLICABLE | not applicable | not applicable | non-deployed CLI | none | none | Rungrid has no production service environment. |
