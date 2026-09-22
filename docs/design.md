# cfs Product and Architecture Design

Status: Implementing

## 1. Product definition

`cfs` is a project-scoped runtime for the official Cloud Foundry CLI. It gives
each development workspace its own Cloud Foundry API target, organization,
space, and authentication state without requiring users or coding tools to
select a named session.

Product statement:

> `cfs` keeps Cloud Foundry CLI state isolated per workspace while preserving
> the native `cf` command-line experience.

Primary users include developers running multiple repositories concurrently
through terminals, Codex, Claude Code, IDE agents, scripts, or CI workers. The
core is tool-agnostic and has no dependency on a particular coding agent.

All product names, commands, configuration keys, documentation, diagnostics,
and error messages are English-only. Localization is not part of the initial
product.

## 2. Product boundaries

`cfs` is:

- A standalone native executable.
- A transparent shim for the `cf` command.
- A deterministic workspace resolver.
- A secure store for separate CF CLI home directories.
- A per-workspace command coordinator.

`cfs` is not:

- A Cloud Foundry CLI plugin.
- A replacement Cloud Foundry client.
- An OAuth or credential manager.
- A named session switcher.
- A Codex, Claude Code, or IDE plugin.
- A synchronization service for credentials.
- A background daemon.

The official Cloud Foundry CLI remains responsible for authentication, token
refresh, API communication, plugins, command semantics, and compatibility with
Cloud Foundry deployments.

## 3. Design principles

1. **Workspace is the default isolation boundary.** One Git worktree maps to
   one CF CLI state directory.
2. **Normal use is implicit.** Users run `cf`; they do not create or select
   sessions.
3. **Implicit does not mean invisible.** The active workspace and target are
   always inspectable.
4. **Delegate instead of reimplementing.** Every CF operation is performed by
   the official CLI.
5. **Fail closed on ambiguity.** An unresolved workspace never falls back to
   the shared default `$HOME/.cf`.
6. **Preserve native behavior.** Arguments, standard streams, terminal mode,
   signals, and exit status pass through unchanged.
7. **Keep credentials out of repositories.** All mutable state remains in a
   user-private operating-system state directory.
8. **Different workspaces run concurrently.** Commands sharing one workspace
   are coordinated to prevent configuration races.
9. **Installation is explicit and reversible.** The official CLI is never
   overwritten, renamed, or deleted.
10. **No telemetry by default.** The product does not collect command
    arguments, target names, credentials, or usage data.

## 4. User experience

### 4.1 Installation

Example future installation flow:

```console
$ go install github.com/zongqichen/cfs/cmd/cfs@latest
$ cfs setup
Found official CF CLI: /opt/homebrew/bin/cf
Installed shim: /Users/alice/.local/share/cfs/shims/cf
Add this directory to the beginning of PATH: /Users/alice/.local/share/cfs/shims
Run 'cfs doctor' to verify the installation.
```

Future Linux packages, Homebrew formulae, and Windows installers will provide
the same logical setup. The command name is `cfs`.

`cfs setup` does not modify a shell profile. It records the canonical path of
the existing official `cf` executable before installing the shim and prints the
PATH change the user can add explicitly.

### 4.2 Normal use

```console
$ cd ~/work/orders
$ cf login --sso -a https://api.example.com
$ cf target -o commerce -s development
$ cf apps
```

There is no `cfs create`, `cfs use`, or `cfs exec` step.

On the first `cf` invocation in a workspace, `cfs` creates an empty private CF
home. The official CLI then behaves as it would with any new `CF_HOME`. Future
shells and coding tools operating in that worktree resolve the same home.

### 4.3 Multiple projects

```text
~/work/orders   -> context 7ce1... -> commerce/development
~/work/payments -> context a03b... -> finance/production
```

The projects can run `cf` commands concurrently without sharing configuration.

### 4.4 Multiple worktrees

Different Git worktrees are separate workspace instances, even when they share
one Git repository:

```text
~/work/orders      -> development context
~/work/orders-prod -> production context
```

This is the recommended way to operate two targets concurrently for one code
base.

### 4.5 Inspection

```console
$ cfs status
Workspace: /Users/alice/work/orders
Source: git-worktree
Context: 7ce1c83f
CF CLI: /opt/homebrew/bin/cf
CF home: /Users/alice/.local/state/cfs/contexts/7ce1c83f.../home
API endpoint: https://api.example.com
Organization: commerce
Space: development
```

`cfs status` obtains target information by invoking the official `cf target`
inside the resolved home. It must not parse, print, or log tokens.

Machine-readable output is available through `cfs status --json`.

## 5. Command surface

The initial control interface is deliberately small:

| Command | Purpose |
| --- | --- |
| `cfs setup` | Locate the official CLI and install the transparent shim. |
| `cfs status` | Show workspace resolution and the current CF target. |
| `cfs doctor` | Validate paths, permissions, CLI compatibility, and state. |
| `cfs reset` | Move the current workspace state to recoverable trash after confirmation. |
| `cfs gc` | Report orphaned workspace state; deletion requires `--apply`. |
| `cfs uninstall` | Remove the shim and PATH integration, preserving state. |
| `cfs version` | Print the `cfs` version and build information. |
| `cfs help [command]` | Show global or command-specific help. |

Global conventions:

- `--json` produces stable JSON for `status`, `doctor`, and `gc`.
- `--quiet` suppresses non-error messages.
- Errors use the form `cfs: <message>`.
- Interactive prompts are never used when standard input is not a terminal.
- Destructive commands require an explicit flag in non-interactive mode.

Named session commands are intentionally excluded.

## 6. Runtime architecture

```text
Terminal / Codex / Claude Code / IDE / CI
                    |
                    | cf <arguments>
                    v
             cfs shim mode
        +-----------+------------+
        | Workspace Resolver     |
        | Context Store          |
        | Lock Manager           |
        | Process Runner         |
        +-----------+------------+
                    |
                    | CF_HOME=<workspace home>
                    | CF_PLUGIN_HOME=<plugin home>
                    v
           Official CF CLI binary
                    |
                    v
             Cloud Foundry APIs
```

One executable supports two invocation modes:

- Invoked as `cf`: transparent shim mode.
- Invoked as `cfs`: control and diagnostic mode.

The installed `cf` shim may be a symbolic link, hard link, or small launcher
that points to the `cfs` executable.

## 7. Execution algorithm

For each invocation of `cf <arguments>`:

1. Load global `cfs` configuration.
2. Resolve the pinned official CF CLI path and reject recursion.
3. Apply explicit compatibility overrides.
4. Resolve the current workspace root.
5. Derive or retrieve the workspace context ID.
6. Validate and create the private state directory.
7. Acquire the workspace process lock.
8. Construct the child environment.
9. Start the official CLI with the original argument vector.
10. Forward standard input, output, error, terminal behavior, and signals.
11. Return the official CLI exit status unchanged.
12. Release the lock.

Conceptual pseudocode:

```go
func runCF(args []string) int {
    realCF := config.RealCFPath
    scope := resolveScope(os.Getwd(), os.Environ())
    context := store.Open(scope)

    lock := context.Lock()
    defer lock.Release()

    env := cloneEnvironment()
    env.Set("CF_HOME", context.CFHome())
    env.SetIfUnset("CF_PLUGIN_HOME", store.SharedPluginHome())
    env.Set("CFS_ACTIVE_CONTEXT", context.ID())

    return runAndForward(realCF, args, env)
}
```

The implementation must never build a shell command string. It invokes the
official binary with an argument array to avoid quoting bugs and injection.

## 8. Workspace resolution

Resolution is deterministic and follows this precedence:

1. `CFS_DISABLE=1`: invoke the official CLI without managed isolation.
2. `CFS_WORKSPACE_ROOT`: use the supplied canonical directory.
3. Existing `CF_HOME`: treat it as an explicit external context and preserve it.
4. Nearest parent containing `.cfs.toml`: use that directory.
5. Git `--show-toplevel`: use the current Git worktree root.
6. No result: fail closed.

Example error:

```text
cfs: no workspace could be resolved; refusing to use the global CF home
Hint: run this command inside a Git worktree or set CFS_WORKSPACE_ROOT.
```

The resolver canonicalizes symlinks and platform-specific path casing before
calculating identity. A nested directory always resolves to its parent
workspace.

### 8.1 Monorepos and non-Git workspaces

An optional `.cfs.toml` marks a directory as an independent workspace:

```toml
version = 1
```

The file contains no credentials or target state and may be committed. The
nearest marker wins over the Git worktree root.

## 9. Workspace identity

The initial identity key is derived from:

```text
SHA-256("cfs:v1" + canonical workspace root + repository fingerprint)
```

The repository fingerprint is a one-way digest of locally available Git
identity information. Credentials embedded in remote URLs must be stripped
before hashing. Raw remote URLs are never written to metadata or logs.

The canonical path keeps separate clones and worktrees isolated. Moving a
workspace creates a new context by default. This is a safe failure mode: it may
require another login, but it cannot silently attach credentials from an
unrelated directory. A future explicit `cfs rebind` operation may support safe
migration.

## 10. State layout

Logical layout:

```text
<state-root>/cfs/
  contexts/
    <full-context-id>/
      home/
        .cf/
          config.json
      metadata.json
  locks/
    <full-context-id>.lock
  plugins/
    .cf/
      plugins/
  registry.json
```

Default state roots:

- Linux: `$XDG_STATE_HOME/cfs`, or `$HOME/.local/state/cfs`.
- macOS: `$HOME/Library/Application Support/cfs`.
- Windows: `%LOCALAPPDATA%\cfs`.

Security requirements:

- Private directories use owner-only permissions where supported.
- CF configuration files retain mode `0600` on Unix-like systems.
- Metadata never contains tokens, passwords, client secrets, or command lines.
- Context IDs are validated fixed-length hexadecimal strings.
- Symbolic-link traversal outside the state root is rejected.

The official CF CLI owns the contents of `home/.cf`. `cfs` treats that
directory as opaque and does not modify or merge `config.json`.

## 11. Plugin policy

By default, installed CF plugins are shared across workspace contexts through a
single managed `CF_PLUGIN_HOME`. This avoids reinstalling the same plugin in
every project while keeping API targets and tokens isolated through `CF_HOME`.

If `CF_PLUGIN_HOME` is already set, `cfs` preserves it. A future strict mode may
isolate plugins per workspace.

Plugins execute with the user's privileges and can receive active CF context
information from the official CLI. Documentation must treat plugin installation
as installation of trusted executable code.

## 12. Concurrency model

Different workspace contexts never share a lock and run concurrently.

The initial implementation uses one exclusive operating-system lock for the
entire lifetime of each official `cf` process in a workspace. This prevents a
target change or token write from racing with a long-running operation.

If the lock cannot be acquired within the configured timeout, `cfs` returns a
temporary-failure exit code and a non-sensitive error:

```text
cfs: this workspace already has an active CF command
Hint: wait for the command to finish or use a separate Git worktree.
```

The lock record may contain a PID and start time, but never the complete command
line. Locks are released by the operating system when the owning process exits.

Long-lived read operations such as logs and SSH may receive a read-only snapshot
mode in a later release. Correctness is preferred over concurrency in the first
release.

Nested execution is marked with `CFS_ACTIVE_CONTEXT` to prevent a plugin or
child process from recursively acquiring the same lock.

## 13. Compatibility behavior

- A pre-existing `CF_HOME` is considered an explicit user decision and is
  preserved unless managed mode is explicitly forced.
- A pre-existing `CF_PLUGIN_HOME` is preserved.
- All unrelated environment variables pass through unchanged.
- CF CLI aliases, plugins, stdin prompts, SSO browser flows, and terminal control
  must behave identically to direct official CLI execution.
- Absolute invocations of the official CLI intentionally bypass `cfs`.
- `CFS_DISABLE=1` provides an explicit one-command bypass.

Support targets should include currently maintained CF CLI major versions. A
compatibility matrix and automated smoke tests determine the supported range;
the shim does not rely on undocumented `config.json` fields.

## 14. Installation safety

`cfs setup` must:

1. Resolve the existing official `cf` before changing PATH.
2. Reject a path that already resolves to the `cfs` binary.
3. Store the canonical official path in the user configuration.
4. Install the shim in a dedicated directory.
5. Print the required PATH change without silently editing shell profiles.
6. Verify that `cf version` works through the shim.
7. Provide exact rollback instructions.

`cfs uninstall` removes only files and shell configuration created by `cfs`. It
does not remove the official CLI or workspace state. State deletion requires a
separate explicit command.

Releases should provide checksums, signed artifacts, an SBOM, and reproducible
build metadata.

## 15. Failure behavior

Failures must be deterministic and actionable:

| Condition | Behavior |
| --- | --- |
| No workspace | Refuse global fallback and explain how to set a root. |
| Official CLI missing | Fail with the configured path and setup instruction. |
| Context permission error | Fail before starting the official CLI. |
| Workspace busy | Return a temporary failure without exposing arguments. |
| Corrupt `cfs` metadata | Preserve CF state and request `cfs doctor`. |
| Official CLI failure | Return its exit status unchanged. |
| Interrupted process | Forward the signal and release the OS lock. |

`cfs` must not automatically delete, repair, or overwrite an existing CF home
after detecting corruption.

## 16. Suggested Go structure

```text
cmd/
  cfs/                 executable entry point and mode dispatch
internal/
  cli/                 control commands and stable output
  config/              global configuration
  workspace/           root discovery and identity
  store/               paths, metadata, and permissions
  lock/                platform-specific process locks
  runner/              child process and signal forwarding
  cfadapter/           CF_HOME and CF_PLUGIN_HOME environment policy
  install/             shim and shell PATH integration
```

Platform-specific files isolate Unix and Windows locking, permissions, signals,
and shim installation behavior.

## 17. MVP scope

The first usable release includes:

- One Go executable named `cfs`.
- Transparent `cf` shim installation.
- Git worktree and `.cfs.toml` workspace detection.
- Isolated persistent `CF_HOME` directories.
- Shared `CF_PLUGIN_HOME`.
- Per-workspace exclusive locking.
- `setup`, `status`, `doctor`, `reset`, `gc`, `uninstall`, and `version`.
- Human-readable English output and stable JSON diagnostics.
- Linux and macOS support.
- Automated tests against supported official CF CLI versions.

Windows support is part of the architecture but may follow after the Unix MVP
unless release requirements demand simultaneous availability.

Explicitly deferred:

- Read-only snapshots for long-running commands.
- Repository move/rebind support.
- Target allow-list policies.
- IDE status-bar integrations.
- Additional stateful CLI adapters.
- Localization.

## 18. Acceptance criteria

The MVP is complete only when all of the following are demonstrated:

1. Two projects can target different orgs and spaces concurrently.
2. A fresh shell in the same project reuses the same CF state.
3. Two worktrees of one repository receive different contexts.
4. Independent non-interactive shells resolve the same project context.
5. A missing workspace cannot write to `$HOME/.cf`.
6. Concurrent commands in one workspace cannot overwrite each other's state.
7. SSO login, plugins, interactive input, terminal signals, and exit codes work
   through the shim.
8. Existing explicit `CF_HOME` and `CF_PLUGIN_HOME` values remain compatible.
9. No token or password appears in `cfs` metadata, logs, or diagnostic output.
10. Uninstalling restores direct access to the official CF CLI without deleting
    user state.

## 19. Architectural decision

The selected design is a workspace-aware external shim around the official CF
CLI. It is preferred over a CF plugin because a plugin cannot transparently
control core commands and their configuration loading. It is preferred over
shell-only environment mutation because coding tools frequently execute commands
in independent shells. It is preferred over a direct API client because that
would duplicate authentication, plugin, and API compatibility responsibilities
already owned by the official CLI.

The durable product contract is therefore:

> Run normal `cf` commands. `cfs` automatically confines their state to the
> current workspace.
