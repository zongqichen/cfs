# Security Policy

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability. Use GitHub's
private vulnerability reporting feature for this repository.

Include the affected version, operating system, reproduction steps, and the
expected security impact. Do not include live Cloud Foundry credentials, access
tokens, refresh tokens, client secrets, or production endpoint details.

## Security model

`cfs` delegates authentication and API communication to the official Cloud
Foundry CLI. It stores each workspace's CF CLI home in a private local state
directory and treats that content as opaque.

The project does not collect telemetry and must never log CF credentials or full
command lines. CF plugins are executable code and should be installed only from
trusted sources.

Managed `CF_HOME` directories contain the same authentication material as an
ordinary official CF CLI home. Owner-only filesystem permissions protect them
from other local users, but the files are not encrypted by `cfs`; backups and
host administrators may still access them.

`cfs reset` and `cfs gc --apply` retain removed contexts in recoverable trash
indefinitely. Moving a context does not revoke its tokens. Log out or revoke the
credentials first when possible, and explicitly remove the relevant trash entry
under the configured cfs state directory when recovery is no longer needed.
Filesystem deletion may not constitute secure erasure on snapshots or SSDs.

Unredacted `cfs status` invokes `cf target` and can print operational metadata,
including local paths, user identity, API endpoint, organization, and space. It
removes `CF_TRACE` from the probe environment and never intentionally requests
tokens. Prefer `cfs status --json --redact` in agent logs, issue reports, and
other shared output.

Official release binaries are built with Go 1.27.1. Source installations require
Go 1.26.8 or newer so the minimum supported toolchain includes the standard
library fixes current when this policy was written.
