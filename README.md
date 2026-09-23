# cfs: isolated Cloud Foundry CLI contexts

[![CI](https://github.com/zongqichen/cfs/actions/workflows/ci.yml/badge.svg)](https://github.com/zongqichen/cfs/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/zongqichen/cfs?include_prereleases&sort=semver)](https://github.com/zongqichen/cfs/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Run independent Cloud Foundry CLI targets in parallel.

The official `cf` CLI keeps one active target in `$HOME/.cf`, so switching it in
one terminal changes it everywhere. `cfs` isolates that state per project or
Git worktree, with optional named contexts when one project needs several
targets.

![Two projects keeping independent Cloud Foundry targets](docs/assets/cfs-demo.gif)

*The demo runs the real `cfs` shim with a local, credential-free CF fixture.
[View the source](docs/demo/demo.tape).*

## Quick start

Install the [official CF CLI](https://github.com/cloudfoundry/cli), then download
`cfs` from [GitHub Releases](https://github.com/zongqichen/cfs/releases) or build
it with Go 1.26.8+:

```sh
go install github.com/zongqichen/cfs/cmd/cfs@latest
cfs setup
```

Prebuilt binaries support Linux and macOS on x86-64 and arm64. Put the shim
directory printed by `cfs setup` first on `PATH`, open a new shell, and run
`cfs doctor`.

Now log in normally from each project:

```sh
cd ~/work/orders
cf login --sso -a https://api.example.com -o commerce -s development
cf apps
```

Another project or Git worktree receives a separate context automatically.
Terminals and agents in the same worktree intentionally share its default
context.

## Multiple targets in one project

The normal `cf` command always uses the workspace's `default` context. Create a
named context only when the same workspace needs another independent target:

```sh
cfs context create prod
cfs -c prod login --sso -a https://api.example.com -o commerce -s production
cfs -c prod apps
```

Each name has its own `CF_HOME` and lock. Names are workspace-local and selected
per command; there is no mutable current context to race over. Inspect them
with `cfs context list` or `cfs context status prod --json --redact`. Unknown
names fail without creating state.

## Existing login

To copy your current global CF target into a workspace once:

```sh
cd ~/work/orders
CFS_DISABLE=1 cf target
cfs import
```

To import into a named context instead:

```sh
cfs context create prod
cfs import --context prod
```

The import is a private snapshot, not a link. It may contain active credentials.
Use `cfs import --yes` in non-interactive automation and `--force` only when you
intend to replace an existing workspace target. `cfs` never imports credentials
silently.

## Coding agents

No agent integration is required. Start Codex, Claude Code, or an IDE agent in
its project or worktree and let it run ordinary `cf` commands. Give an agent an
exact named command such as `cfs -c prod apps` when it needs a non-default
target. For diagnostics safe to attach to agent logs, use:

```sh
cfs status --json --redact
cfs context status prod --json --redact
```

The optional [cfs Agent Skill](.agents/skills/cfs/SKILL.md) teaches agents to
discover existing contexts, fail closed on ambiguity, and preserve user
authorization. Install that directory as `.agents/skills/cfs` for Codex. For
Claude Code, copy it to `.claude/skills/cfs` or symlink that path to the
canonical directory. No global agent settings are changed.

To make invocation explicit, add `Use $cfs for Cloud Foundry context
selection.` to `AGENTS.md`, or `Use /cfs for Cloud Foundry context selection.`
to `CLAUDE.md`.

## How it works

```text
cf command      -> cfs shim -> workspace default CF_HOME -> official cf CLI
cfs -c NAME ... -> cfs      -> workspace named CF_HOME   -> official cf CLI
```

`cfs setup` places a transparent `cf` shim on `PATH`. For each invocation, the
shim resolves the current workspace, selects its private state, and delegates to
the official CLI. Named commands take the same path with an explicit context.
Authentication, token refresh, plugins, API calls, signals, and exit codes
remain the official CLI's responsibility.

Git worktrees are detected automatically. For a directory that is not a Git
worktree, create this marker at its root:

```toml
version = 1
```

Alternatively, select a workspace for one command:

```sh
CFS_WORKSPACE_ROOT=/workspace cf apps
```

## Commands

```text
cfs setup           Install the transparent cf shim
cfs status          Show the current workspace and target
cfs context         Create, list, inspect, or remove named contexts
cfs import          Import the global context into default or a named context
cfs doctor          Diagnose the installation and workspace
cfs reset           Move this workspace's state to trash
cfs gc              Find stale workspace state
cfs uninstall       Remove the shim without deleting state
cfs version         Print version information
cfs help [command]  Show help
```

Run `cfs help context` for the named-context commands.

Use `CFS_DISABLE=1 cf ...` to bypass isolation for one command. `cfs` collects no
telemetry. Workspace state and recoverable trash can contain active tokens; see
[Security](SECURITY.md).

## Development

```sh
make check
make security
make release-check
make smoke
make agent-smoke
CFS_REAL_CF=/path/to/official/cf make e2e
```

See [testing](docs/testing.md), [design](docs/design.md), the
[changelog](CHANGELOG.md), and the [release guide](docs/releasing.md).
Contributions are welcome; read
[CONTRIBUTING.md](CONTRIBUTING.md). Licensed under [Apache-2.0](LICENSE), like
the official CF CLI.
