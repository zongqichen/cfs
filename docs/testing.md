# Testing

`make check` runs the portable unit, process, race, and vet checks. `make smoke`
runs the official CF CLI with isolated local configuration but makes no network
requests.

Run the full protocol test with an official CF CLI binary:

```sh
CFS_REAL_CF=/absolute/path/to/cf make e2e
```

The E2E suite installs the current cfs source into a temporary bin directory,
runs `cfs setup`, and connects the official CLI to an in-process TLS mock of
Cloud Controller and UAA. All homes, state, credentials, repositories, and
worktrees are synthetic and deleted after the test. The mock never records
request bodies or authorization headers.

It verifies:

- interactive password and SSO-passcode login, org/space targeting, and
  authenticated API requests;
- global-context import, two parallel project contexts, process reuse, and
  same-project locking;
- Git worktrees, `.cfs.toml`, `CFS_WORKSPACE_ROOT`, and explicit `CF_HOME`;
- fail-closed behavior, explicit bypass, redacted diagnostics, metadata secrecy,
  explicit plugin homes, exit-code forwarding, and import overwrite protection;
- setup, doctor, reset, garbage collection, state preservation, uninstall, and
  restored direct CLI access.

CI downloads a checksum-pinned CF CLI version and runs this suite on Linux.
It does not replace a smoke test against a live foundation, enterprise identity
provider, or third-party plugin. Signal forwarding remains covered by the
focused process tests in `make check`.
