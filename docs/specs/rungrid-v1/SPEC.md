# Rungrid v1 implementation record

Status: implemented and locally validated; delivery gates remain

## Purpose

This document preserves the rationale and delivery history for Rungrid v1. The
authoritative product and command contract is [`CLI_SPEC.md`](../../../CLI_SPEC.md).
This record intentionally does not duplicate that contract.

## Accepted direction

Rungrid extracts a reproducible development workspace from any particular
repository. A project manifest describes infrastructure and application
services. Process Compose owns process lifecycle through a detached,
project-scoped runtime. Warp is the sole graphical adapter in v1; the same
lifecycle remains usable without a graphical terminal on macOS and Linux.

The visible workspace has three stable layers:

1. an Overview tab attached read-only to Process Compose, including selectable
   process logs;
2. a live Versions tab for lifecycle, listener, and source-control state; and
3. one tab for each configured tab-owned service, in manifest order.

Infrastructure normally uses workspace activation and starts during `up`.
Applications normally use tab activation and remain disabled until a service
tab acquires an exclusive, generation-scoped session. Process Compose still
supervises the underlying process; the tab foreground follows its logs and owns
the right to start and stop it. This separation keeps lifecycle state truthful
in Overview while preserving direct per-tab control.

## Material decisions

- The manifest uses `rungrid/v1`; machine output uses `rungrid/output/v1`.
- Project identity is a persisted slug plus random suffix. Absolute checkout
  paths are runtime inputs, never identity material.
- Structured process arguments and the interactive trigger are separate. This
  permits a familiar trigger while retaining an exact supervised command.
- Process Compose is required in the range `>=1.120.0,<2.0.0` because v1 relies
  on disabled processes, daemon lifecycle, the Unix-socket client, and remote
  read-only TUI behavior.
- Generated files live in project-scoped XDG state, carry ownership and content
  hashes, use restrictive permissions, and are replaced atomically.
- Warp tab files are derived artifacts. Regeneration may replace only artifacts
  whose ownership metadata and prior hash still match.
- External services are observed but never started or stopped by Rungrid.
- The Overview is the Process Compose TUI. Rungrid does not own a competing
  dashboard or a separate all-logs tab in v1.
- Additional graphical terminal adapters and command-free multi-pane workspaces
  are deliberately outside v1.
- Default-branch history rewriting is excluded from implementation lanes. The
  neutral contract is delivered normally; historical objects and archived pull
  request references may retain earlier content.

## Delivery record

The accepted plan proposed a separate issue, branch, and pull request for each
stage. The repository implementation was completed as one dependency-ordered
candidate on `GH-3`; it must not be represented as four independently reviewed
deliveries. A reviewer may require the candidate to be split before merge. The
consumer-repository cutover remains a separate delivery after a release
candidate is published.

### Stage 1: neutral contract and core

Status: implemented in `GH-3`.

- Replace the product contract with neutral version 2.
- Establish the Go command surface, manifest model and merge rules, validation,
  project identity, XDG state, ownership-safe atomic generation, planning, and
  doctor checks.
- Add sanitization and repository contract tests.

### Stage 2: runtime and manifest-owned environment

Status: implemented in `GH-3`.

- Compile Process Compose configuration and implement detached runtime identity,
  native/Compose wrappers, dependency and health semantics, environment
  providers, sessions and locks, and lifecycle commands.

### Stage 3: Warp, Versions, and service-tab ownership

Status: implemented in `GH-3`.

- Generate ordered Warp Tab Configs, implement read-only Overview, Versions,
  managed zsh trigger interception, tab registration, safe reopening, and
  uninstall.

### Stage 4: onboarding and release candidate

Status: implemented in `GH-3`; release publication is gated.

- Add resumable interactive onboarding and non-interactive initialization,
  documentation, CI, completions, release packaging, provenance, and release
  candidate validation.

### Stage 5: legacy-workspace dogfood and cutover

Status: blocked pending review, merge, a chosen repository license, a published
release candidate, and controlled graphical validation. This stage is performed
in the consumer repository, not this repository.

- Express the legacy workspace as a manifest, replace active wrappers without
  duplicating service inventory, retain isolated rollback material for one
  release cycle, and prove graphical/headless parity before stable release.

## Implementation discoveries

- Current Warp Tab Configs are TOML files. Generated files therefore use the
  ordered names `00_overview.toml`, `01_versions.toml`, and manifest-ordered
  service TOML files.
- Unix-domain socket path limits can be exceeded by legitimate XDG roots.
  Process Compose binds the socket through a short relative path from the
  generation directory while the runtime record retains the verified absolute
  identity.
- Process Compose commands are generated as Rungrid wrapper invocations. User
  argument vectors are never interpolated into Process Compose's shell command
  field.
- `up --headless` creates a headless effective generation even when the source
  manifest selects Warp, so graphical files are not an accidental side effect.
- The Go toolchain minimum is 1.25.12 because the earlier candidate produced
  reachable standard-library vulnerability findings.
- Bubble Tea's original transitive `go-localereader` tag predates that module's
  MIT license file. The dependency graph pins the first licensed upstream
  revision and the repository gate rejects dependency archives without license
  or notice material.
- Process Compose can write structured diagnostics to stderr before returning a
  valid JSON response on stdout, notably on a minimal Linux host without an XDG
  configuration directory. The client keeps these streams separate so
  diagnostics cannot corrupt machine-readable state while failed command
  output remains redacted.

## Validation record

Local validation completed on macOS with Process Compose 1.120.0:

- `make check`: formatting, vet, unit/integration tests, race tests, contract
  sanitization, native build, and Darwin/Linux amd64/arm64 builds.
- `make lint`: zero `golangci-lint` findings.
- `make vuln`: no reachable vulnerabilities reported by `govulncheck`.
- `tests/end-to-end/local/run.sh`: real detached Process Compose lifecycle,
  runtime identity tamper rejection, active-generation protection, exclusive
  tab sessions, stop/restart, cleanup, and immutable ignored evidence.
- `goreleaser check` and `make release-snapshot`: release configuration and all
  four target archives validated locally. Signing and SBOM creation are covered
  by CI/release configuration; the local snapshot skips an unavailable tool.
- Fresh-install initialization, validation, planning, generation, and a real
  Process Compose dry run were exercised in temporary directories.
- A Linux/arm64 container reproduction with Process Compose 1.120.0 exercised
  workspace startup, tab-session ownership, `Disabled` to `Running` to
  `Completed` transitions, interrupt handling, status JSON, and shutdown.

The pull-request workflow includes the same Process Compose version and uploads
immutable run evidence. Hosted checks exposed and now cover Linux socket-path
reporting and separation of Process Compose diagnostics from JSON responses.
The graphical Warp smoke, signing, SBOM generation, published release, and
consumer-workspace parity are not observed and are not passing claims.

## Outcome

Rungrid v1 is implemented as a review candidate with the neutral contract,
portable manifest, lifecycle runtime, Warp/headless presentation, onboarding,
tests, CI, and release packaging. Default-branch history was not rewritten:
repository guardrails prohibit force pushing or mutating the default branch,
and archived pull-request refs or direct object URLs could retain old content
even after a branch rewrite. Release publication remains gated on review,
merge, a license decision, hosted checks, and controlled Warp validation. The
consumer migration remains a separate post-RC change.
