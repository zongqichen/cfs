# Changelog

All notable changes to cfs are documented here. This project follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). The command-line
contract may still change while cfs is pre-1.0.

## [Unreleased]

### Added

- Added `cfs import` for explicitly copying an existing global CF context into
  a workspace, with confirmation, automation, and overwrite safeguards.
- Added redacted status output for shared logs and workspace-target guidance to
  `cfs doctor`.
- Added an end-to-end suite that exercises the official CF CLI against isolated
  local Cloud Controller and UAA mocks, including login, targeting, parallel
  workspaces, worktrees, import, locking, reset, garbage collection, and
  uninstall flows.
- Added CodeQL, dependency review, Dependabot, secret and vulnerability scans,
  repository templates, and manual release approval controls.

### Changed

- Windows packaging is deferred; supported release targets remain Linux and
  macOS on amd64 and arm64.
- Source builds now require Go 1.26.8 or newer; release builds use Go 1.27.1.
- Global-target discovery now reads the CF configuration without invoking the
  official CLI, so inspection cannot rewrite global state.
- Destructive state operations now warn that recoverable trash may retain active
  credentials.

### Fixed

- CF configurations with an empty `Target` are no longer treated as logged in.
- Nested shim execution now accepts only a valid managed context and matching
  `CF_HOME`.
- State paths, permissions, and configuration imports are hardened against
  symbolic-link traversal and unsafe files.

## [0.1.1] - 2026-09-22

### Added

- Added global and command-specific help for the cfs control commands.

### Changed

- Split the CLI core into focused command, environment, executable, filesystem,
  context, and store components.

### Fixed

- Compare canonical executable paths when discovering the official CLI and the
  installed shim, including paths reached through symbolic links.

## [0.1.0] - 2026-09-22

### Added

- Initial pre-release of the transparent `cf` shim.
- Automatic workspace resolution for Git worktrees, `.cfs.toml` markers, and
  `CFS_WORKSPACE_ROOT`.
- Isolated per-workspace `CF_HOME` state, shared plugin storage, and
  per-workspace process locking.
- `setup`, `status`, `doctor`, `reset`, `gc`, `uninstall`, and `version` control
  commands.
- Linux and macOS release builds, checksums, provenance, CI, smoke tests, and an
  Apache-2.0 license.

[Unreleased]: https://github.com/zongqichen/cfs/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/zongqichen/cfs/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/zongqichen/cfs/releases/tag/v0.1.0
