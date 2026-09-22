# cfs

[![CI](https://github.com/zongqichen/cfs/actions/workflows/ci.yml/badge.svg)](https://github.com/zongqichen/cfs/actions/workflows/ci.yml)

Project-scoped Cloud Foundry CLI state, with the native `cf` experience.

`cfs` gives every development workspace an isolated Cloud Foundry API target,
organization, space, and authentication state. It delegates every Cloud
Foundry operation to the official CLI.

```text
~/work/orders   -> commerce/development
~/work/payments -> finance/production
```

Users keep running normal commands:

```console
$ cd ~/work/orders
$ cf login --sso
$ cf target -o commerce -s development
$ cf apps
```

There are no named sessions and no context-switching commands. A Git worktree
is the default isolation boundary. This works across interactive terminals,
Codex, Claude Code, IDE tools, scripts, and other processes that start fresh
shells for each command.

> [!IMPORTANT]
> `cfs` is currently an early implementation. Test it with non-production
> Cloud Foundry targets before relying on it for operational workflows.

## How it works

After setup, a small shim named `cf` appears before the official CLI on `PATH`:

```text
cf <arguments>
      |
      v
cfs workspace resolver
      |  CF_HOME=<private workspace home>
      v
official cf <arguments>
```

The shim resolves the current Git worktree or nearest `.cfs.toml` marker,
selects a private `CF_HOME`, obtains a per-workspace lock, and starts the
official CF CLI with the original arguments and terminal streams.

The official CLI remains responsible for login, token refresh, API calls,
plugins, and every CF command. `cfs` does not parse or modify its
`config.json`.

## Install from source

Requirements:

- Go 1.22 or newer.
- The official Cloud Foundry CLI already installed.
- Linux or macOS for the current MVP.

```console
$ go install github.com/zongqichen/cfs/cmd/cfs@latest
$ cfs setup
```

By default, the shim is installed in:

```text
$HOME/.local/share/cfs/shims
```

Place that directory before the official CF CLI directory on `PATH`:

```sh
export PATH="$HOME/.local/share/cfs/shims:$PATH"
```

Add the line to the appropriate shell profile, start a new shell, and verify:

```console
$ cfs doctor
$ command -v cf
$ cf version
```

Use `cfs setup --real-cf /absolute/path/to/cf` if automatic discovery finds the
wrong executable. `cfs setup` never overwrites or renames the official CLI.

## Commands

```text
cfs setup       Install and configure the transparent cf shim
cfs status      Show workspace resolution and the current CF target
cfs doctor      Diagnose configuration and installation problems
cfs reset       Move the current workspace state to recoverable trash
cfs gc          Find state for workspaces that no longer exist
cfs uninstall   Remove the shim without deleting workspace state
cfs version     Print version information
```

Normal Cloud Foundry operations always use `cf`, not `cfs exec`:

```console
$ cf login --sso -a https://api.example.com
$ cf target -o my-org -s my-space
$ cf push
```

## Non-Git and monorepo workspaces

Place a `.cfs.toml` marker at the desired workspace root:

```toml
version = 1
```

The nearest marker takes precedence over the Git worktree root. The file never
contains credentials or mutable target state and may be committed.

For one-off automation, set an explicit root:

```console
$ CFS_WORKSPACE_ROOT=/workspace cf apps
```

## Safety behavior

- No detected workspace means no command: `cfs` refuses to fall back to the
  shared `$HOME/.cf`.
- Different workspaces run concurrently. Commands in the same workspace are
  serialized to protect CF CLI configuration.
- State directories are private to the current operating-system user.
- `cfs reset` and `cfs gc --apply` move state to recoverable trash.
- Existing `CF_HOME` and `CF_PLUGIN_HOME` values are treated as explicit user
  overrides and are preserved.
- `CFS_DISABLE=1 cf ...` explicitly bypasses managed isolation.
- No telemetry is collected.

## Development

```console
$ make check
$ make build
```

The implementation uses the Go standard library and currently has no runtime
dependencies.

See [the architecture design](docs/design.md) for the complete product
contract, state model, security constraints, and acceptance criteria.
