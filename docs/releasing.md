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
$ go test -race ./...
$ make release-check
$ make smoke
```

`make smoke` uses an installed official CF CLI without credentials. Before a
release, also perform the opt-in manual acceptance test against a non-production
Cloud Foundry foundation:

1. Log in with `cf login --sso` from a new workspace.
2. Confirm a fresh shell in that workspace retains the target.
3. Run two coding agents in different workspaces and confirm that they retain
   different org and space targets.
4. Confirm the user's global `$HOME/.cf` target is unchanged.

Never put credentials, tokens, target configuration, or live acceptance output
in release artifacts or CI logs.

## Release automation

The tag-only GitHub Actions workflow uses GoReleaser v2. It:

1. triggers only for tags matching `v*`;
2. checks out full history with `fetch-depth: 0`;
3. reruns tests and the release-build check;
4. builds the four supported archives with `CGO_ENABLED=0` and `-trimpath`;
5. injects version, commit, and build date into `main.version`, `main.commit`, and
   `main.buildDate`;
6. publishes `checksums.txt` and GitHub artifact attestations; and
7. uses narrowly scoped workflow permissions (`contents: write`, plus
   `id-token: write` and `attestations: write` when attestations are enabled).

All actions are pinned to reviewed commit SHAs. Do not create a release from an
unreviewed local commit.

## Distribution

A semantic version tag makes the command installable through the Go toolchain:

```console
$ go install github.com/zongqichen/cfs/cmd/cfs@v0.1.0
```

After pushing the immutable tag, request it through the public Go proxy:

```console
$ GOPROXY=proxy.golang.org go list -m github.com/zongqichen/cfs@v0.1.0
```

GitHub Release archives are the recommended initial distribution. A separate
`zongqichen/homebrew-tap` repository can provide `brew install` after the first
release exists and its artifact URLs and checksums are stable.

## Repository controls

- Protect `main` and require the Linux, macOS, and release-build CI jobs.
- Validate the tag-only release workflow in a dry run before publishing.
- Describe the early-project support boundary in pre-1.0 release notes.
- Keep Git tags immutable; publish a new version for every correction.
