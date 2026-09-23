# Releasing cfs

`cfs` releases are marked as pre-releases while the pre-1.0 CLI contract is
still settling. Choose the next version according to semantic versioning.

## Supported artifacts

Publish archives for the platforms that currently implement workspace locking:

- Linux amd64
- Linux arm64
- macOS amd64
- macOS arm64

Do not publish a Windows binary until the Windows lock implementation and its
integration tests exist. Each release should include SHA-256 checksums. Supply
chain provenance and an SBOM are recommended before calling a release stable.

## Required gates

Release only from a clean, reviewed commit on `main` after all of these pass:

```console
$ make check
$ make security
$ make release-check
$ make smoke
```

`make smoke` uses an installed official CF CLI without credentials. If the
active `cf` command is already the cfs shim, pass the official executable
explicitly: `CFS_REAL_CF=/absolute/path/to/cf make smoke`.

Before a release, also perform the opt-in manual acceptance test against a
non-production Cloud Foundry foundation:

1. Log in globally, run `cfs import` from a new workspace, and confirm the
   imported target without printing credentials.
2. Confirm the global configuration is unchanged by the import.
3. Confirm a fresh shell in that workspace retains the target.
4. Run two coding agents in different workspaces and confirm that they retain
   different org and space targets.
5. Confirm a failed command in a fresh workspace suggests `cfs import` without
   modifying the global target.

Never put credentials, tokens, target configuration, or live acceptance output
in release artifacts or CI logs.

## Release automation

Pushing a tag never publishes a release. A maintainer must manually run the
GitHub Actions `Release` workflow from `main` and enter an existing semantic
version tag. The workflow rejects a tag that is not the current `main` commit or
already has a GitHub Release. Configure required reviewers on the `release` GitHub
environment when more than one maintainer is available.

The manually started workflow uses GoReleaser v2. It:

1. validates that the requested semantic version tag is the current `main` commit;
2. checks out that tag with full history;
3. reruns tests, vulnerability and secret scans, and the release-build check;
4. builds the four supported archives with `CGO_ENABLED=0` and `-trimpath`;
5. injects version, commit, and build date into `main.version`, `main.commit`, and
   `main.buildDate`;
6. publishes `checksums.txt` and GitHub artifact attestations; and
7. uses narrowly scoped workflow permissions (`contents: write`, plus
   `id-token: write` and `attestations: write` when attestations are enabled).

All actions and security tools are pinned to reviewed versions. Do not create a
release from an unreviewed local commit.

## Distribution

A semantic version tag makes the command installable through the Go toolchain,
but does not publish GitHub archives on its own:

```console
$ go install github.com/zongqichen/cfs/cmd/cfs@v0.1.0
```

After pushing the immutable tag and manually running the Release workflow,
request it through the public Go proxy:

```console
$ GOPROXY=proxy.golang.org go list -m github.com/zongqichen/cfs@v0.1.0
```

GitHub Release archives are the recommended initial distribution. A separate
`zongqichen/homebrew-tap` repository can provide `brew install` after the first
release exists and its artifact URLs and checksums are stable.

## Repository controls

- Protect `main` and require pull requests plus the Linux, macOS, minimum-Go,
  release-build, security, dependency-review, and CodeQL jobs.
- Use merge commits for reviewed pull requests; do not combine that policy with
  GitHub's linear-history requirement.
- Run the manual release workflow only from `main` and require the `release`
  environment's approval gate when repository staffing permits.
- Describe the early-project support boundary in pre-1.0 release notes.
- Keep Git tags immutable; publish a new version for every correction.
