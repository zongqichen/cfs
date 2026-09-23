# Contributing to cfs

Thanks for improving cfs. Keep changes focused, portable across Linux and
macOS, and compatible with the documented minimum Go version.
Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before opening a pull request

- Search existing issues and pull requests.
- Report vulnerabilities through GitHub's private vulnerability reporting, not
  a public issue.
- Never include a real CF configuration, token, client secret, endpoint, or
  unredacted `cfs status` output.

Run the local checks:

```sh
make check
make release-check
make security
```

Run `make smoke` when changing shim discovery, environment handling, process
execution, or context isolation. The smoke test must use a non-production CF CLI
configuration.

## Pull requests

- Add tests for behavior changes and update user-facing documentation.
- Use a short imperative title with one of these prefixes when practical:
  `feat:`, `fix:`, `docs:`, `test:`, `ci:`, `chore:`, `refactor:`, or
  `security:`.
- Keep generated files and unrelated cleanup out of the change.
- Treat CI, security scans, and reviewer feedback as merge gates.

AI-assisted contributions are welcome. Contributors remain responsible for
reviewing generated changes, validating them, respecting third-party licenses,
and ensuring that prompts and logs contain no credentials.

By submitting a contribution, you agree that it is licensed under the project's
[Apache-2.0 license](LICENSE).
