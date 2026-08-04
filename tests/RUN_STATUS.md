# Validation run status

| Suite | Environment | Current status | Latest attempt | Latest pass | Source/deployment | Run ID | Evidence | Active finding |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Headless lifecycle | local | PASS | 2026-08-04T12:17:40Z | 2026-08-04T12:17:40Z | clean `a4ffcef` / no deployment | `20260804T121733Z-014138` | `tmp/2026-08-04/rungrid-headless-e2e/2/` (ignored local evidence) | Mixed-service and tab-only workspaces passed against Process Compose 1.120.0 after the help presentation redesign. |
| Graphical Warp smoke | local macOS | BLOCKED | not run | none | local source / no deployment | none | none | Requires a controlled user-visible Warp session. |
| Production | production | NOT_APPLICABLE | not applicable | not applicable | non-deployed CLI | none | none | Rungrid has no production service environment. |
