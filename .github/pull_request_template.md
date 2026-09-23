## Summary

<!-- What changed, and why? -->

## Validation

- [ ] `make check`
- [ ] `make release-check`
- [ ] Security-sensitive changes: `make security`
- [ ] Shim behavior changes: `make smoke`

## Checklist

- [ ] Tests and documentation cover the behavior change.
- [ ] Notable changes are recorded under `Unreleased` in `CHANGELOG.md`, or this
      change does not require an entry.
- [ ] No credentials, tokens, CF configuration, or sensitive logs are included.
- [ ] The change is focused and does not publish a release.
