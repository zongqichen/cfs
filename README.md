# cfs: isolated Cloud Foundry CLI contexts

[![CI](https://github.com/zongqichen/cfs/actions/workflows/ci.yml/badge.svg)](https://github.com/zongqichen/cfs/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/zongqichen/cfs?include_prereleases&sort=semver)](https://github.com/zongqichen/cfs/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Run multiple Cloud Foundry CLI targets in parallel—one isolated `cf` context per
project or Git worktree.

The official `cf` CLI stores one active API endpoint, organization, space, and
login in the shared `$HOME/.cf`. Running `cf login` or `cf target` in one terminal
therefore changes the target for every other terminal, script, and coding agent.
`cfs` gives each workspace a private `CF_HOME` while preserving the normal `cf`
command.

```text
~/work/orders   -> commerce/development
~/work/payments -> finance/production
```

## Why cfs

- **Same CLI:** keep using `cf login`, `cf target`, `cf push`, plugins, and scripts.
- **Automatic isolation:** the current Git worktree selects its own API, org,
  space, and login state—no sessions to name or contexts to switch.
- **Parallel-safe:** different workspaces run concurrently; commands in the same
  workspace are serialized.
- **Agent-ready:** the working directory provides stable context across the fresh
  shells used by Codex, Claude Code, and IDE agents.
- **Official CLI underneath:** authentication, token refresh, plugins, and Cloud
  Foundry API calls remain the responsibility of the official `cf` CLI.

> [!IMPORTANT]
> `cfs` is an early implementation. Use non-production targets while evaluating it.

## Install

Requirements: Go 1.22+, the [official Cloud Foundry CLI](https://github.com/cloudfoundry/cli),
and Linux or macOS.

```console
$ go install github.com/zongqichen/cfs/cmd/cfs@latest
$ cfs setup
```

Prebuilt Linux and macOS archives are available from
[GitHub Releases](https://github.com/zongqichen/cfs/releases).

Add the printed shim directory to your shell profile before the official CF CLI:

```sh
export PATH="$HOME/.local/share/cfs/shims:$PATH"
```

Start a new shell and verify the installation:

```console
$ cfs doctor
$ command -v cf
```

`command -v cf` should resolve to `$HOME/.local/share/cfs/shims/cf`. Use
`cfs setup --real-cf /absolute/path/to/cf` if automatic discovery selects the
wrong executable.

## Use

Keep using the official CLI commands inside each project:

```console
$ cd ~/work/orders
$ cf login --sso -a https://api.example.com -o commerce -s development
$ cf apps
```

In another project or worktree:

```console
$ cd ~/work/payments
$ cf login --sso -a https://api.example.com -o finance -s production
$ cf apps
```

Commands in either workspace automatically use that workspace's target and login.

## Coding agents

`cfs` is agent-friendly, not agent-specific. Start Codex, Claude Code, or an IDE
agent inside a project and let it use normal `cf` commands. No plugin, prompt, or
agent-specific session API is required.

The agent must inherit the shim `PATH` and have network access to the CF API. A
one-time `command -v cf` check inside the agent should resolve to the `cfs` shim.
Use `cfs status --json` and `cfs doctor --json` for machine-readable checks.

## Workspace boundaries

A Git worktree is the default boundary. For a non-Git project or an independent
directory inside a monorepo, add this file at the desired root:

```toml
# .cfs.toml
version = 1
```

For one command, an explicit root takes precedence:

```console
$ CFS_WORKSPACE_ROOT=/workspace cf apps
```

## Management commands

```text
cfs setup       Install and configure the cf shim
cfs status      Show the workspace and current CF target
cfs doctor      Check the installation
cfs reset       Move the current workspace state to trash
cfs gc          Find state for workspaces that no longer exist
cfs uninstall   Remove the shim without deleting workspace state
cfs version     Print version information
cfs help [command]  Show global or command-specific help
```

`cfs` delegates login, token refresh, API calls, and plugins to the official CF
CLI. It does not parse credentials, and it fails closed outside a workspace
instead of falling back to `$HOME/.cf`. Set `CFS_DISABLE=1` for an explicit
one-command bypass. No telemetry is collected.

## Development

```console
$ make check
$ make release-check
$ make smoke
```

The implementation uses only the Go standard library at runtime. See the
[architecture](docs/design.md), [release guide](docs/releasing.md), and
[security policy](SECURITY.md).

## License

Apache License 2.0, the same license used by the official Cloud Foundry CLI.
See [LICENSE](LICENSE) and [NOTICE](NOTICE).
