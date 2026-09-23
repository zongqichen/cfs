# Repository guidance

These instructions apply to the entire repository.

## Safety

- Never read, copy, commit, or print a user's real `$HOME/.cf/config.json`.
- Use temporary `CF_HOME`, `CFS_STATE_HOME`, and workspace directories in tests.
- Keep tokens, client secrets, endpoint details, and unredacted status output out
  of logs and fixtures.
- Do not create tags or publish releases without explicit maintainer approval.

## Changes

- Preserve the transparent `cf` shim behavior and fail closed when managed state
  cannot be validated.
- Keep commands non-interactive when an explicit automation flag is provided.
- Prefer small functions, standard-library packages, and contextual errors.
- Format Go files with `gofmt` and update tests and documentation with behavior.

## Validation

Run `make check` and `make release-check`. Run `make security` for security,
dependency, state, filesystem, process, or workflow changes. Run `make smoke`
for changes to shim discovery, environment handling, execution, or isolation.
