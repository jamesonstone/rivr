# Validation run status

| Suite | Environment | Current status | Latest attempt | Latest pass | Source/deployment | Run ID | Evidence | Active finding |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Headless lifecycle | local | PASS | 2026-08-04T17:20:43Z | 2026-08-04T17:20:43Z | clean `abaeb92` / no deployment | `20260804T172043Z-034267` | `tmp/2026-08-04/rungrid-headless-e2e/3/` (ignored local evidence) | Mixed-service and tab-only workspaces passed against Process Compose 1.120.0 through the bounded evidence runner; output was 246 bytes of the 64 MiB limit and no descendant remained. |
| Graphical Warp smoke | local macOS | BLOCKED | not run | none | local source / no deployment | none | none | Requires a controlled user-visible Warp session. |
| Production | production | NOT_APPLICABLE | not applicable | not applicable | non-deployed CLI | none | none | Rungrid has no production service environment. |
