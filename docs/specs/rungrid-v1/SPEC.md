# Rungrid v1 implementation record

Status: v1 candidate implemented; workspace lifecycle extension locally validated

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

## Workspace lifecycle extension

Multi-repository workspaces are a core portability requirement. A manifest may
live in one repository while describing sibling repositories under a common
relative workspace root. Infrastructure setup and final teardown are ordered,
one-shot workspace lifecycle operations rather than long-running Process
Compose services.

This extension is delivered in two review lanes. Rungrid issue `#10` owns the
neutral manifest, runtime, recovery, command, test, and documentation changes.
Only after those prerequisites are reviewable will a separate consumer issue,
branch, specification, and ready pull request adopt the feature. Rungrid source
and fixtures remain free of consumer names, paths, and commands.

Material decisions for this extension:

- The manifest directory and workspace root are distinct. `workspace.root`
  defaults to `.`, is relative to the manifest directory, may name an ancestor,
  and is resolved with symlink-aware containment checks.
- The source manifest establishes the workspace boundary before imports are
  traversed. Imported fragments cannot redefine it; the adjacent ignored local
  overlay remains anchored to the manifest directory.
- Services, Compose files, and environment-provider paths resolve from the
  workspace root. Stable identity and deterministic generation use only the
  relative declaration; absolute resolved paths remain machine-local runtime
  data.
- `lifecycle.before_up` and `lifecycle.after_down` are sequential structured
  argument vectors. They reuse environment providers and redaction, but are
  never emitted as Process Compose processes or Warp tabs.
- The lifecycle journal records teardown intent before the first prerequisite
  mutates external state. Required teardown survives prerequisite failure,
  supervisor startup failure, signals, process crashes, and a missing runtime
  record.
- One project-scoped lifecycle lock serializes `up`, `down`, recovery, and
  uninstall. Service-level `start` and `stop` preserve their existing scope and
  never invoke global hooks.
- Cleanup attempts every teardown command, retains `cleanup-required` on any
  failure, and must complete under the recorded generation before a different
  generation may start.
- Exact PID and socket identity remain hard safety gates. A stale or ambiguous
  runtime is not permission to rerun prerequisites, signal a process, delete a
  socket, or discard teardown state.

The implementation sequence is contract and manifest loading, deterministic
planning, journal and executor, lifecycle command integration, recovery and
uninstall behavior, then generic tests and delivery validation. The consumer
lane follows only after the neutral lane is complete.

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

Status: implemented and locally validated on `GH-10`. Consumer adoption remains
a separate repository lane and is not part of this branch.

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
- The source manifest must establish and validate the workspace root before an
  import path can be resolved. Imports and the ignored local overlay are then
  merged without permitting either input to change that boundary.
- A valid tab-only Process Compose generation has every process disabled. The
  detached runtime may still be ready even though no managed service is running
  until a session explicitly starts one.
- Lifecycle cleanup cannot depend on a runtime record. A durable teardown
  obligation must remain actionable after supervisor startup fails or its
  runtime identity disappears.
- Replacing an advisory-lock file while a process is waiting can split mutual
  exclusion across inodes. The lock holder therefore validates the acquired
  inode and reacquires when the path was replaced.
- Environment-provider paths receive the same execution-time, symlink-aware
  workspace boundary check as static service and lifecycle paths.
- A headless plan must derive its artifact list from the effective terminal
  mode, not from the graphical mode in source configuration.
- Process Compose 1.120 rejects the intuitive `warning` log level after
  generation. Rungrid now validates the exact accepted levels in both its
  semantic validator and published schema so this fails before lifecycle
  mutation.
- A top-level executable check is insufficient for common structured command
  vectors such as `env ... direnv exec . make dev`. Planning and Doctor now
  expose each supported wrapper layer plus the tab trigger without attempting
  to parse opaque shell command strings.
- The local command symlink recipe must fail closed when privilege elevation or
  link replacement fails. It verifies the final link target before reporting
  success, matching the existing local `kit`, `yp`, and `kp` command layout.

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
- Workspace-root and lifecycle tests cover sibling repositories, symlink
  escapes, overlay replacement, exact argument vectors, timeouts, cancellation,
  redaction, lock replacement, journaling, rollback, missing-runtime cleanup,
  retry and no-op teardown, and uninstall refusal while cleanup is required.
- Real mixed-service and tab-only headless runs prove prerequisites precede the
  supervisor, teardown follows it, repeated `up` does not repeat prerequisites,
  and Process Compose remains the managed-service lifecycle authority.
- A follow-up real headless run after Process Compose log-level and structured
  executable discovery changes passed both mixed-service and tab-only suites;
  ignored evidence is recorded as run `20260803T152058Z-082251`.
- `make build` was exercised under a temporary writable prefix and produced an
  exact `bin/rungrid` symlink. The existing `/usr/local/bin/rungrid` link uses
  the same canonical-repository layout as `kit`, `yp`, and `kp`; no
  administrator-authenticated link replacement was claimed from the worktree.

The pull-request workflow includes the same Process Compose version and uploads
immutable run evidence. Hosted checks exposed and now cover Linux socket-path
reporting and separation of Process Compose diagnostics from JSON responses.
The graphical Warp smoke, local SBOM generation, action-workflow linting,
signing, published release, and consumer-workspace parity are not observed and
are not passing claims. Their required local tools or controlled graphical
environment were unavailable where applicable.

## Outcome

Rungrid v1 is implemented as a review candidate with the neutral contract,
portable multi-repository workspace boundary, crash-safe one-shot lifecycle,
Process Compose runtime, Warp/headless presentation, onboarding, tests, CI, and
release packaging. The neutral implementation is ready for a separately owned
consumer cutover lane; this outcome does not claim consumer parity.

Default-branch history was not rewritten:
repository guardrails prohibit force pushing or mutating the default branch,
and archived pull-request refs or direct object URLs could retain old content
even after a branch rewrite. Release publication remains gated on review,
merge, a license decision, hosted checks, and controlled Warp validation. The
consumer migration remains a separate post-RC change.
