# Testing

`make check` runs the portable unit, process, race, and vet checks. CI executes
the test suite on Linux and macOS. `make smoke` runs the official CF
CLI with isolated local configuration but makes no network requests.

Run the full protocol test with an official CF CLI binary:

```sh
CFS_REAL_CF=/absolute/path/to/cf make e2e
```

The E2E suite installs the current cfs source into a temporary bin directory,
runs `cfs setup`, and connects the official CLI to an in-process TLS mock of
Cloud Controller and UAA. All homes, state, credentials, repositories, and
worktrees are synthetic and deleted after the test. The mock never records
request bodies or authorization headers. The suite runs with Go's race detector.

Its independent user journeys verify:

- automatic official-CLI discovery, setup, a fresh shell, repeat setup,
  uninstall, and restored direct CLI access;
- interactive password and SSO-passcode login, org/space targeting, and
  identity-bound authenticated API requests;
- five concurrent workspace logins and app queries, fresh-process reuse, and
  same-workspace locking;
- multiple named contexts in one workspace, including concurrent login and app
  queries, independent and shared locks, directed import, lifecycle commands,
  and fail-closed name validation;
- Git worktrees, `.cfs.toml`, `CFS_WORKSPACE_ROOT`, and explicit `CF_HOME`;
- fail-closed behavior, explicit bypass, redacted diagnostics, metadata secrecy,
  explicit plugin homes, exit-code forwarding, and import overwrite protection;
- interrupted-process lock recovery, import, doctor, reset, garbage collection,
  and state preservation.

CI downloads checksum-pinned CF CLI archives and runs this suite on Linux and
macOS.
It does not replace a smoke test against a live foundation, enterprise identity
provider, or third-party plugin. Signal forwarding remains covered by the
focused process tests in `make check`.
