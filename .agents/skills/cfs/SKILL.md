---
name: cfs
description: Safely select an existing cfs Cloud Foundry context and route CF CLI commands in cfs-managed workspaces, including parallel terminal or coding-agent workflows. Use for workspace-default or named-context selection, not for general Cloud Foundry deployment guidance.
---

# Use cfs contexts

Treat the cfs CLI as the source of truth. Do not inspect or edit CF configuration files.

1. Run `cfs context list --json` to discover context names.
2. For an unqualified request, inspect the workspace default with `cfs status --json --redact`. For an explicitly named context, require an exact existing name and inspect it with `cfs context status <name> --json --redact`.
3. If the requested context is absent or the request does not identify one context unambiguously, stop and ask the user which existing name to use. Do not guess, create, or retarget a context.
4. Run an authorized command through `cf <arguments...>` for `default`, or exactly `cfs -c <name> <cf arguments...>` for a named context.

Never select a context through `cf target`, a manual `CF_HOME`, `CFS_DISABLE`, or a direct official-CF binary path. Keep shared diagnostics both JSON-formatted and redacted.

Selecting a context grants no authority to log in, deploy, import, create or remove contexts, change targets, or perform any other mutation. Run such commands only when the user has authorized that specific operation. If inspection shows the selected context is unavailable or not logged in, report that state and ask before changing it.
