```text
██████╗ ██╗   ██╗███╗   ██╗ ██████╗ ██████╗ ██╗██████╗
██╔══██╗██║   ██║████╗  ██║██╔════╝ ██╔══██╗██║██╔══██╗
██████╔╝██║   ██║██╔██╗ ██║██║  ███╗██████╔╝██║██║  ██║
██╔══██╗██║   ██║██║╚██╗██║██║   ██║██╔══██╗██║██║  ██║
██║  ██║╚██████╔╝██║ ╚████║╚██████╔╝██║  ██║██║██████╔╝
╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝ ╚═════╝ ╚═╝  ╚═╝╚═╝╚═════╝

                         one workspace. truthful lifecycle.
```

Rungrid turns a multi-service development workspace into one observable,
controllable system. A portable `.rungrid.yaml` is compiled into a detached
Process Compose runtime and, on macOS, an ordered Warp workspace:

1. Overview — read-only Process Compose TUI and selectable logs.
2. Versions — live process, listener, branch, commit, and worktree state.
3. Service tabs — one exclusive lifecycle owner per tab-owned application.

<!-- BEGIN KIT-MANAGED README BADGES -->
[![Last commit](https://img.shields.io/github/last-commit/jamesonstone/rungrid)](https://github.com/jamesonstone/rungrid/commits) [![Open issues](https://img.shields.io/github/issues/jamesonstone/rungrid)](https://github.com/jamesonstone/rungrid/issues) [![Pull requests](https://img.shields.io/github/issues-pr/jamesonstone/rungrid)](https://github.com/jamesonstone/rungrid/pulls) [![CI](https://github.com/jamesonstone/rungrid/actions/workflows/ci.yml/badge.svg)](https://github.com/jamesonstone/rungrid/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/jamesonstone/rungrid)](https://github.com/jamesonstone/rungrid/releases)
<!-- END KIT-MANAGED README BADGES -->

## Requirements

- macOS with Warp and zsh for the graphical workspace;
- macOS or Linux for headless use; and
- Process Compose `>=1.120.0,<2.0.0`.

Native and Compose services may add their own executable requirements. Run
`rungrid doctor` for a redacted, project-specific report.

## Quick start

Create a manifest with the guided onboarding flow:

```sh
rungrid init
rungrid plan
rungrid doctor
rungrid up
```

Headless operation uses the same lifecycle:

```sh
rungrid up --headless --no-open
rungrid status
rungrid session api
rungrid down
```

Inside a Warp service tab, Ctrl-C stops that tab's service and returns to the
same managed zsh. Running the configured exact trigger, such as `make dev`,
restarts it. Other invocations of that executable pass through unchanged.

## Manifest

```yaml
api_version: rungrid/v1
kind: Workspace
project:
  name: Example Workspace
  slug: example-workspace
  id: example-workspace-k7m4q2
terminal:
  mode: warp
services:
  - name: database
    source: compose
    activation: workspace
    compose:
      file: compose.yaml
      service: database
  - name: api
    source: native
    activation: tab
    working_directory: services/api
    run:
      argv: [go, run, ./cmd/server]
    terminal:
      trigger_argv: [make, dev]
    depends_on:
      database: running
```

Workspace-owned services start during `up`. Tab-owned services remain disabled
in Process Compose until an exclusive `rungrid session` owns them. External
services are readiness dependencies only and are never started or stopped.

The complete product and safety contract is [CLI_SPEC.md](CLI_SPEC.md). Durable
implementation rationale lives in
[docs/specs/rungrid-v1/SPEC.md](docs/specs/rungrid-v1/SPEC.md).

## Commands

Rungrid v1 provides `init`, `doctor`, `plan`, `generate`, `up`, `open`,
`attach`, `versions`, `status`, `logs`, `session`, `start`, `stop`, `down`,
`uninstall`, `config`, `completion`, and `version`. Every JSON-capable command
uses a `rungrid/output/v1` envelope.

Generated files, runtime identity, locks, logs, and terminal ownership live in
project-scoped XDG state. Rungrid verifies ownership hashes, PID start identity,
and Unix-socket identity before lifecycle mutation. `uninstall` removes only
verified project-owned state and Warp Tab Configs.

## Development

```sh
make build
make run ARGS="version"
make install
make check
make test-e2e
make release-snapshot
```

`make build` writes `./bin/rungrid` and links it as
`/usr/local/bin/rungrid`, matching the local command convention. Override
`PREFIX` to use another prefix, for example `make build PREFIX="$HOME/.local"`.
The link step requests administrator privileges only when the destination is
not writable and refuses to replace a regular file. `make run ARGS="..."`
executes the repository binary, and `make install` installs it with the active
Go toolchain. `make check` checks formatting, vets, runs unit/race and
dependency-license tests, verifies the specification sanitization contract,
and builds macOS/Linux targets without changing the global link. The opt-in
end-to-end suite launches a real Process Compose v1 runtime in temporary XDG
state.

## Maintainers

Maintained with 🪖 and ❤️ by [Jameson](https://github.com/jamesonstone)
(`jamesonstone`).
