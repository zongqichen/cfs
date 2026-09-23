# cfs: isolated Cloud Foundry CLI contexts

[![CI](https://github.com/zongqichen/cfs/actions/workflows/ci.yml/badge.svg)](https://github.com/zongqichen/cfs/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/zongqichen/cfs?include_prereleases&sort=semver)](https://github.com/zongqichen/cfs/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Project-scoped Cloud Foundry CLI contexts for parallel terminals, Git worktrees,
and coding agents.

The official `cf` CLI keeps one active target in `$HOME/.cf`. Changing it in one
terminal changes it everywhere. `cfs` gives each workspace an isolated
`CF_HOME` while preserving the normal `cf` command.

```text
~/work/orders   -> commerce/development
~/work/payments -> finance/production
```

## Install

Requires Linux or macOS on x86-64 or arm64 and the
[official CF CLI](https://github.com/cloudfoundry/cli). Building from source also
requires Go 1.26.8+.

```sh
go install github.com/zongqichen/cfs/cmd/cfs@latest
cfs setup
```

Add the directory printed by `cfs setup` to the beginning of `PATH`, start a new
shell, then verify:

```sh
cfs doctor
command -v cf
```

Prebuilt binaries are available from
[GitHub Releases](https://github.com/zongqichen/cfs/releases).

## Use

Log in normally from a project:

```sh
cd ~/work/orders
cf login --sso -a https://api.example.com -o commerce -s development
cf apps
```

Every terminal and agent in that Git worktree now shares the same context. A
different project or worktree gets a different context automatically.
This works unchanged in Codex, Claude Code, IDE agents, and shell scripts.

Already logged in with the global CF CLI? Import that context once:

```sh
cd ~/work/orders
CFS_DISABLE=1 cf target
cfs import
```

The import is a snapshot, not a link. It copies the global CF configuration,
including its credentials, into private workspace state. Use `cfs import --yes`
for non-interactive automation and `--force` only to replace an active workspace
target. An empty workspace detects an available global context and suggests the
command, but never imports it silently.

For a non-Git directory, add a `.cfs.toml` file at the workspace root:

```toml
version = 1
```

Or select a root for one command:

```sh
CFS_WORKSPACE_ROOT=/workspace cf apps
```

## Commands

```text
cfs setup           Install the transparent cf shim
cfs status          Show the current workspace and target
cfs import          Import the global context into this workspace
cfs doctor          Diagnose the installation and workspace
cfs reset           Move this workspace's state to trash
cfs gc              Find stale workspace state
cfs uninstall       Remove the shim without deleting state
cfs version         Print version information
cfs help [command]  Show help
```

Use `cfs status --json --redact` for agent logs. Use `CFS_DISABLE=1 cf ...` to
bypass isolation for one command.

`cfs` delegates authentication, token refresh, plugins, and API calls to the
official CF CLI. It never silently imports credentials and collects no
telemetry. Workspace state and recoverable trash can contain active tokens; see
[SECURITY.md](SECURITY.md).

## Development

```sh
make check
make security
make release-check
make smoke
CFS_REAL_CF=/path/to/official/cf make e2e
```

The E2E suite uses the real CLI against an isolated local mock; see
[testing](docs/testing.md). See also the [changelog](CHANGELOG.md),
[design](docs/design.md), and [release guide](docs/releasing.md). Licensed under
[Apache-2.0](LICENSE), like the official CF CLI. Contributions are welcome; see
[CONTRIBUTING.md](CONTRIBUTING.md) and the
[Code of Conduct](CODE_OF_CONDUCT.md).
