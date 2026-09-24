# Cloud Foundry CLI context options

The official Cloud Foundry CLI stores its active API, organization, space, and
authentication state under `CF_HOME`. Terminals that share the same `CF_HOME`
therefore share one current target. Several tools address this in different
ways.

| Approach | Isolation and selection | Keeps normal `cf` commands | Best fit |
| --- | --- | --- | --- |
| Manual `CF_HOME` | Set a different directory in each shell or process. | Yes | Small scripts or users who want direct control. |
| [cf-targets-plugin](https://github.com/guidowb/cf-targets-plugin) | Save named targets and restore one as the current target. | Yes, with plugin commands for switching | Sequential switching among saved targets. |
| [cfctx](https://github.com/nkuhn-vmw/cfctx) | Select a named, per-context `CF_HOME` in the current shell. | Yes, after shell integration and selection | Tanzu foundation workflows that also use Ops Manager, BOSH, or CredHub. |
| `cfs` | Resolve an isolated `CF_HOME` from the project or Git worktree on every invocation; names are optional and command-scoped. | Yes, through a transparent shim | Parallel projects, terminals, scripts, and coding agents. |

Choose `cfs` when the working directory should determine the default target and
multiple projects must run concurrently without changing shared CLI state. Use
a named context only when one project needs more than one target:

```sh
cf apps
cfs -c production apps
```

`cfs` does not replace the official CLI, Cloud Foundry authentication, or its
plugin system. It selects `CF_HOME` and delegates the command. See the
[architecture design](design.md) for the trust and isolation model.

The original concurrent-session problem and the manual `CF_HOME` solution are
documented in [cloudfoundry/cli#330](https://github.com/cloudfoundry/cli/issues/330).
