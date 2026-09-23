# Security Policy

## Supported versions

Only the latest release receives security fixes. This project is pre-1.0, so
fixes are not routinely backported to earlier releases.

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability. Use the
[private vulnerability reporting form](https://github.com/zongqichen/cfs/security/advisories/new).

Include the affected version, operating system, reproduction steps, and the
expected security impact. Do not include live Cloud Foundry credentials, access
tokens, refresh tokens, client secrets, or production endpoint details.

## Security model

`cfs` delegates authentication and API communication to the official Cloud
Foundry CLI. It stores each workspace context's CF CLI home in a private local
state directory and treats that content as opaque.

The project does not collect telemetry and must never log CF credentials or full
command lines. CF plugins are executable code and should be installed only from
trusted sources.

`cfs update` and the cached interactive update notice contact only the public
GitHub Releases API. Requests contain no Cloud Foundry target, credential,
workspace, context, or command data. The cache contains only public release
versions and timestamps. Set `CFS_NO_UPDATE_CHECK=1` to disable passive checks.
Update discovery never downloads or executes a replacement binary.

Managed `CF_HOME` directories contain the same authentication material as an
ordinary official CF CLI home. Owner-only filesystem permissions protect them
from other local users, but the files are not encrypted by `cfs`; backups and
host administrators may still access them.

`cfs import` makes an explicit, one-time copy of `$HOME/.cf/config.json` into
the default context or an existing named context. The source file
can contain access tokens, refresh tokens, and client secrets. It requires an
interactive confirmation or `--yes`, rejects symbolic links and source files
readable by other users, writes the destination atomically with mode `0600`, and
never prints the file contents. `--force` can replace an active workspace target
but does not revoke the credentials it replaces. Import never runs implicitly.

`cfs reset`, `cfs context remove`, and `cfs gc --apply` retain removed contexts
in recoverable trash indefinitely. Moving a context does not revoke its tokens.
Log out or revoke the credentials first when possible, and explicitly remove
the relevant trash entry under the configured cfs state directory when recovery
is no longer needed. Filesystem deletion may not constitute secure erasure on
snapshots or SSDs.

Unredacted `cfs status` and `cfs context status` invoke `cf target` and can print
operational metadata, including local paths, user identity, API endpoint,
organization, and space. They remove `CF_TRACE` from the probe environment and
never intentionally request tokens. Prefer their `--json --redact` form in
agent logs, issue reports, and other shared output.

Official release binaries are built with Go 1.27.1. Source installations require
Go 1.26.8 or newer so the minimum supported toolchain includes the standard
library fixes current when this policy was written.
