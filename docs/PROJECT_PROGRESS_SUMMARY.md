# Project progress summary

## Project intent

Rungrid turns a portable workspace manifest into one authoritative Process
Compose lifecycle, usable through ordered Warp tabs or headlessly on macOS and
Linux. This file is the compact repository-memory index; follow its pointers
before loading broader history.

## Global constraints

- [`CONSTITUTION.md`](CONSTITUTION.md) is the durable project contract.
- [`../CLI_SPEC.md`](../CLI_SPEC.md) is the authoritative product and command
  contract.
- [`specs/rungrid-v1/SPEC.md`](specs/rungrid-v1/SPEC.md) records v1 rationale,
  implementation discoveries, validation, and delivery gates.
- Process Compose is the only lifecycle authority; terminal presentation never
  maintains a competing service state.
- Release and consumer-workspace changes remain separate mutation boundaries.

## FEATURE PROGRESS TABLE

| Feature | Source | Highest completed artifact | Status |
| --- | --- | --- | --- |
| `rungrid-v1` | `docs/specs/rungrid-v1/SPEC.md` | Integrated implementation and local validation candidate on issue/branch `GH-3`. | Ready for pull-request review; history rewrite, graphical smoke, license selection, release publication, and consumer cutover remain gated. |

## FEATURE SUMMARIES

### Rungrid v1

- **STATUS**: review candidate
- **INTENT**: Replace repository-owned development-workspace scripts with a
  neutral manifest and one truthful Process Compose lifecycle.
- **IMPLEMENTED**: Portable manifest processing, safe generated state,
  workspace/tab/external activation, detached lifecycle, exclusive sessions,
  ordered Warp tabs, headless operation, Versions, onboarding, tests, CI, and
  release packaging.
- **OPEN ITEMS**: Review and merge, choose a license, observe hosted checks,
  perform a controlled Warp smoke, publish the release candidate, then deliver
  consumer migration separately. The protected-history rewrite is excluded.
- **POINTER**: `docs/specs/rungrid-v1/SPEC.md`

## Current implementation

- The Go CLI implements the complete documented command surface, strict
  manifest merge and validation, XDG state, deterministic generation,
  Process Compose supervision, exact native and Compose execution, exclusive
  sessions, Warp/headless presentation, Versions, onboarding, and uninstall.
- Unit, integration, race, golden, contract, fake-executable, and real
  Process Compose end-to-end suites cover the repository-owned boundaries.
- GitHub Actions covers code-level gates, verified Process Compose lifecycle
  evidence, cross-builds, vulnerability analysis, release snapshots, SBOMs,
  and tag signing.

## Validation state

- Local formatting, vet, tests, race tests, sanitization, lint,
  vulnerability analysis, Darwin/Linux cross-builds, real Process Compose
  lifecycle, project contract validation, and release snapshot are the required
  handoff gates.
- Hosted workflow results, a controlled macOS Warp smoke, checksum signing,
  SBOM publication, and release installation are observed only after the
  corresponding PR/tag actions run.
- Production validation is not applicable because Rungrid is a local CLI.

## Last updated

- 2026-08-01: Implemented and locally validated the integrated Rungrid v1
  review candidate; recorded remaining privacy, delivery, release, graphical,
  and consumer-migration gates.
