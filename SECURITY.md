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
