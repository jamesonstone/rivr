# CONSTITUTION

## PRINCIPLES

- A Rungrid workspace is declared by a portable manifest, not by scripts owned
  by a particular consumer repository.
- Process Compose is the single authority for managed-service lifecycle. Every
  Rungrid view and service command must report or change that same runtime
  rather than infer a parallel service state.
- Interactive terminal ownership and process supervision are separate: a tab
  may own the right to start and stop a service while Process Compose remains
  authoritative for its lifecycle and logs.
- One-shot workspace prerequisites and teardown are Rungrid operations. Their
  journal, not Process Compose, is authoritative for ordering and recovery.

## CONSTRAINTS

- Manifest and output contracts use `rungrid/v1` and `rungrid/output/v1`.
- Project identity must not encode or hash an absolute developer path.
- The manifest directory and workspace root are distinct. The portable root is
  relative to the manifest, while resolved paths are machine-local and must
  remain inside the symlink-aware workspace boundary.
- Subprocesses use argument vectors. User commands, environment values, and
  paths must not be interpolated into shell command strings.
- Secrets resolve only at execution time and must be redacted from errors,
  plans, generated artifacts, registrations, and evidence.
- External services are observed but never started or stopped by Rungrid.
- Runtime state is project-scoped, private, atomic, and fail-closed. PID,
  process-start, socket, generation, owner, and content-hash checks protect
  every mutation boundary they identify.
- A project-scoped lock serializes global lifecycle mutation. Once startup may
  have changed external state, its journal retains teardown intent until every
  required cleanup command succeeds, even when the runtime record is missing.
- Global lifecycle hooks are exact, sequential argument vectors. They are not
  managed services or terminal tabs and never run for individual service
  `start` or `stop` commands.
- Generated terminal files may be replaced or removed only when their ownership
  marker and last recorded content hash match.
- Headless operation must not require or generate graphical terminal state.

### Kit-Managed Baseline Rules

<!-- BEGIN KIT-MANAGED BASELINE RULES -->
- Treat `docs/CONSTITUTION.md` as the canonical project contract.
- Keep `AGENTS.md`, `CLAUDE.md`, and `.github/copilot-instructions.md` aligned with the repo-local docs tree.
- Treat `docs/notes/<feature>` as optional source material, not canonical truth; promote durable decisions into `SPEC.md`, `docs/CONSTITUTION.md`, or durable references.
- Use native agent planning for research, clarification, design, and implementation planning.
- Before implementation, inspect code and repository memory; create or adopt `SPEC.md` when material rationale exists.
- After validation, curate feature rationale, project invariants, reusable practices, and domain knowledge into their scope-appropriate canonical documents.
- Allow a justified `not required` repository-memory decision when code and tests preserve the complete durable truth.
- Keep every version-control-eligible handwritten implementation/source and test file at 300 physical lines or less.
- Before delivery, audit the complete affected source/test scope; whole-project reconcile and scheduled maintenance audit the entire repository.
- Exclude documentation files, all `docs/**`, all `.kit/**`, `.kit.yaml`, ignored files, vendored dependencies, and proven generated files.
- Split oversized files by semantic responsibility while preserving stable public entry points and behavior; never use minification or arbitrary numbered chunks to claim compliance.
<!-- END KIT-MANAGED BASELINE RULES -->

## CHANGE CLASSIFICATION

<!-- all work falls into one of two tracks — classify before acting -->

### Repository-Memory Work

<!-- use when: consequential product rationale, architecture, cross-component behavior, or historical decisions must survive -->
<!-- workflow: native plan → create/adopt SPEC.md before code → implement → validate → curate repository memory -->
<!-- legacy staged documents: BRAINSTORM.md, legacy SPEC.md, PLAN.md, TASKS.md only when explicitly chosen -->

### Ad Hoc (Lightweight)

<!-- use when: bug fixes, security reviews, refactors, dependency updates, config changes, small refinements -->
<!-- workflow: understand → implement → verify -->
<!-- docs: update practical canonical docs when behavior changes -->
<!-- do not create feature SPEC.md solely for ceremony; report a justified not-required memory decision -->

### Ad Hoc with Existing Specs

<!-- if change touches code with existing spec docs: update them when rationale, behavior, requirements, or approach changes -->
<!-- leave them unchanged when code and tests communicate the complete durable truth -->

## NON-GOALS

- Rungrid v1 does not implement a product-owned dashboard, an all-logs tab,
  additional graphical terminal adapters, Windows support, or command-free
  multi-pane workspaces.
- Rungrid does not own consumer-repository rollback material or rewrite
  repository history as part of normal implementation delivery.

## DEFINITIONS

- **Workspace-owned service:** a managed service started during `rungrid up`.
- **Tab-owned service:** a disabled managed process started only while an
  exclusive generation-scoped service session owns it.
- **External service:** a readiness dependency observed without lifecycle
  mutation.
- **Generation:** an immutable, content-addressed set of derived runtime and
  terminal artifacts for a validated manifest.
- **Workspace root:** the relative manifest declaration whose resolved,
  symlink-aware directory bounds all workspace-owned execution paths.
- **Lifecycle command:** an ordered one-shot prerequisite or teardown command
  owned by Rungrid rather than Process Compose.
- **Lifecycle journal:** the crash-safe project record that proves the active
  generation, completed prerequisites, teardown obligation, and cleanup result.
- **Overview:** the read-only remote Process Compose TUI and its selectable
  service logs.
- **Versions:** the live service, listener, Git branch, commit, and worktree
  state view.
