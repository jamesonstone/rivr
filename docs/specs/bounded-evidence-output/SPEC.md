---
kit_metadata_version: 1
artifact: "spec"
workflow_version: 3
phase: "complete"
feature:
  id: "bounded-evidence-output"
  slug: "bounded-evidence-output"
  dir: "bounded-evidence-output"
relationships:
  - type: builds_on
    target: rungrid-v1
references:
  - id: local-e2e-runner
    name: Local end-to-end evidence runner
    type: code
    target: tests/end-to-end/local/run.sh
    relation: implements
    read_policy: must
    used_for: immutable local lifecycle evidence
    status: active
  - id: testing-reference
    name: Project testing reference
    type: documentation
    target: docs/references/testing.md
    relation: constrains
    read_policy: must
    used_for: reusable evidence-output policy
    status: active
  - id: platform-consumer
    name: Platform Rungrid adoption pull request
    type: pull_request
    target: https://github.com/lsmc-bio/platform/pull/42
    relation: depends_on
    read_policy: must
    used_for: consumer remediation
    status: active
skills: []
delivery_intent: issue_branch_pr_ready
---
# SPEC

## PURPOSE

Prevent local and CI evidence capture from exhausting disk or feeding an
artifact back into itself while preserving complete bounded diagnostics,
truthful result metadata, child exit status, cleanup, and immutable run
directories.

## CONTEXT

- The Rungrid local end-to-end runner redirected a child command into
  `output.txt`, then replayed the complete file with `cat` on both success and
  failure. If the runner's own output was redirected to that same artifact,
  the reader consumed newly appended bytes indefinitely.
- The unsafe pattern originated in Rungrid and was copied into Platform's open
  Rungrid adoption lane. Three observed consumer runs consumed approximately
  189 GiB and ended with `ENOSPC`; ordinary Rungrid lifecycle evidence was only
  246 bytes.
- The exact external redirecting process is unavailable. The fix therefore
  removes the unsafe invariant and bounds data before disk exhaustion instead
  of detecting one redirect shape or checking size afterward.
- Non-interactive dependency and machine-readable subprocess calls can also
  allocate unbounded memory through `Output` or `CombinedOutput`. Intentional
  interactive streams have different semantics and must remain streaming.
- Rungrid issue #16 and branch `GH-16` own the authoritative fix. Platform
  issue #41 and pull request #42 own the required consumer repair.

## REQUIREMENTS

- Treat `output.txt` as the sole full-output evidence artifact. Never replay
  it unboundedly to stdout or stderr on any success, failure, signal, or
  cleanup path.
- Limit each evidence artifact to 64 MiB before writes exceed that bound. Do
  not offer an unlimited mode.
- On overflow, stop the complete child process group, wait for it, preserve
  the bounded artifact, write final metadata, and return stable exit code 74.
- Preserve child exit codes for ordinary success and failure. Map HUP, INT, and
  TERM to their conventional shell statuses while recording the signal.
- Write `result.json` for success, command failure, handled signal, and output
  overflow. Record `failure_kind`, `output_bytes`, `output_limit_bytes`, and
  `output_truncated` in every result.
- Keep terminal output to concise status metadata and the exact evidence path.
- Reserve immutable positive run numbers atomically; repeated and concurrent
  executions must never reuse or overwrite a directory.
- Bound non-interactive subprocess capture used for dependency checks,
  supervisor setup, Process Compose JSON, and machine inspection. Preserve
  stdout/stderr separation, redaction, JSON decoding, and command error codes.
- Keep intentional `logs --follow`, sessions, and read-only attach streams
  uncapped.
- Give the CI end-to-end job an explicit timeout and upload the immutable run
  directory even when output limiting fails.
- Apply the same terminal-output and evidence-size contract to Platform pull
  request #42 before calling the incident remediated.
- Keep every affected handwritten source and test file at or below 300
  physical lines.

### Non-goals

- Reproducing the historical disk usage or depending on the missing redirect
  command.
- Reducing the 64 MiB limit after a command completes.
- Treating normal interactive user output as evidence capture.
- Changing Rungrid service lifecycle, manifest, terminal ownership, or
  Process Compose streaming behavior.
- Replacing Platform's existing issue, branch, or pull request.

## ACCEPTED PLAN

1. Add focused regression tests with bounded temporary output for success,
   failure, signals, overflow, descendant cleanup, and concurrent run
   allocation before replacing the shell capture path.
2. Add a small portable Go evidence runner beneath `tests/` that reserves the
   run directory, writes bounded combined output once, owns the child process
   group, finalizes `result.json`, and prints only concise status/path data.
3. Reduce the shell runner to stable invocation and configuration of the Go
   helper; retain the existing real Process Compose lifecycle command.
4. Extract a reusable bounded subprocess capture primitive and apply it to
   non-interactive supervisor, Process Compose, dependency, and machine-state
   calls without changing streaming surfaces.
5. Add the CI timeout, reusable testing policy, and validation evidence.
6. Repair Platform's existing GH-41 lane with the same bounded helper
   contract, focused regressions, and preserved Process Compose/fake Docker
   acceptance scenario.
7. Run focused, repository-wide, end-to-end, release, source-size, and hosted
   checks; curate the observed outcome and deliver ready review state.

## DECISIONS

- Use a 64 MiB hard limit. It is more than 270,000 times the observed 246-byte
  normal artifact while still placing a deterministic ceiling far below the
  189 GiB incident.
- Enforce the bound in the writer, before another byte reaches disk. Filesystem
  quotas, post-command checks, `tee`, inode comparisons, and platform-specific
  shell limits do not remove the self-feeding invariant portably.
- Give the evidence helper its own child process group. Killing only the
  immediate process can leave descendants producing output or holding
  resources after the recorded failure.
- Use exit code 74 for evidence overflow, matching an I/O failure without
  colliding with ordinary Go test success or its usual exit code 1.
- Preserve full output only in the bounded artifact. Terminal diagnostics are
  metadata, not a second log transport.

## DISCOVERIES

- Keeping the shell wrapper as the signal-facing process required explicit
  HUP, INT, and TERM forwarding to the temporary helper binary. The helper then
  owns and terminates the isolated child process group; focused tests prove the
  shell and helper do not leave descendants behind.
- A single mutex-protected writer is required for combined stdout/stderr.
  `os/exec` may call separate stream writers concurrently, so the shared 4 MiB
  capture primitive must serialize writes while preserving distinct buffers
  for Process Compose JSON and diagnostics.
- Every production and test `Output` or `CombinedOutput` call could be replaced
  without changing interactive commands. `logs --follow`, sessions, and attach
  already use explicit streaming writers and remain intentionally uncapped.
- Platform's current `main` added active-worktree service selection after pull
  request #42 branched. That runtime contract is separate from evidence
  safety; the consumer harness can be repaired and validated on GH-41 without
  pretending its existing base conflict is resolved by this incident fix.

## VALIDATION

- PASS: focused evidence-runner tests prove success and child-failure exit-code
  preservation, output written once, terminal output below 1 KiB, handled
  signals, stable overflow status 74, 32 KiB test-limit enforcement,
  descendant cleanup, and concurrent immutable run allocation.
- PASS: `go test ./internal/subprocess` proves separated stdout/stderr,
  preserved exit status, bounded combined capture, and process termination at
  the 4 MiB machine-output limit.
- PASS: repository search finds no remaining Go `Output` or `CombinedOutput`
  calls. Interactive logs, sessions, and attach retain explicit streams.
- PASS: `make check`, including formatting, vet, full tests, race tests,
  sanitization, dependency licenses, native build, and four cross-builds.
- PASS: `make lint` with zero findings and `make vuln` with no reachable
  vulnerabilities.
- PASS: clean-source `tests/end-to-end/local/run.sh` against Process Compose
  1.120.0. Run `20260804T172043Z-034267` at commit `abaeb92` produced a
  246-byte `output.txt`, recorded the 64 MiB limit, and left no helper,
  Rungrid, or Process Compose descendant.
- PASS: `make release-snapshot` built all four archives from clean commit
  `abaeb92`.
- PASS: Platform's focused Python regressions, complete consumer contract
  validator, and real Process Compose/fake Docker lifecycle passed with an
  exact `abaeb92` Rungrid binary. Platform run 2 produced bounded local
  evidence and left no runtime behind.
- PASS: `git diff --check`, a no-findings working-tree Gitleaks scan, and
  `kit reconcile --all --output-only`; Kit audited 99 eligible handwritten
  source/test files with none above 300 physical lines.

## OUTCOME

Rungrid evidence output is now written once to an immutable artifact with a
64 MiB pre-write ceiling. Success, command failure, signals, and overflow
produce truthful final metadata; overflow and handled signals terminate and
wait for the complete child process group. Runner terminal output is limited
to status and the evidence path.

All non-interactive machine and dependency subprocess capture is limited to
4 MiB while preserving Process Compose stdout/stderr separation, redaction,
JSON decoding, and existing command error mapping. Interactive streams remain
unchanged. CI has an explicit 15-minute end-to-end timeout and retains its
always-upload evidence behavior.

The matching Platform GH-41 consumer repair is implemented and locally
validated in pull request #42's existing worktree. Its separate base conflict
with Platform's newer active-worktree runtime contract remains visible and is
not misrepresented as part of this safety fix.

## REPOSITORY MEMORY

Decision: created

Rationale: The incident boundary, pre-write limit, child-process ownership,
consumer obligation, and separation between evidence capture and interactive
streaming are consequential cross-component safety decisions that code and
tests alone cannot fully explain.

Artifacts:

- `docs/specs/bounded-evidence-output/SPEC.md`
- `docs/references/testing.md`
