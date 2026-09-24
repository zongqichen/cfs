# Changelog

All notable changes to cfs are documented here. This project follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). The command-line
contract may still change while cfs is pre-1.0.

## [Unreleased]

## [0.3.1] - 2026-09-24

### Changed

- Renamed the repository and Go module to `cloud-foundry-cli-contexts` for
  clearer discovery while preserving the `cfs` command name.
- Context metadata now requires explicit workspace and context identities
  instead of accepting the earlier incomplete schema.

## [0.3.0] - 2026-09-23

### Added

- Added `cfs update` with agent-safe JSON output and cached, opt-out update
  notices that never run from the transparent `cf` shim.

### Changed

- Documented one-command installation of the optional cfs Agent Skill.

## [0.2.0] - 2026-09-23

### Added

- Added an optional Agent Skill for safe cfs context discovery and explicit
  named-context routing in Codex, Claude Code, and other Agent Skills clients.
- Added workspace-local named contexts for safely running multiple Cloud
  Foundry targets in one project through `cfs -c <name> ...`.
- Added `cfs context create|list|status|remove` and named-context import through
  `cfs import --context <name>`.
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
- Included an experimental Windows runtime implementation in source. Windows
  remains unsupported, is not exercised in CI, and is excluded from release
  artifacts.

### Changed

- Reworked the user documentation around a concise quick start and a
  reproducible multi-workspace terminal demo.
- Source builds now require Go 1.26.8 or newer; release builds use Go 1.27.1.
- Global-target discovery now reads the CF configuration without invoking the
  official CLI, so inspection cannot rewrite global state.
- Destructive state operations now warn that recoverable trash may retain active
  credentials.

### Fixed

- CF configurations with an empty `Target` are no longer treated as logged in.

### Security

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

[Unreleased]: https://github.com/zongqichen/cloud-foundry-cli-contexts/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/zongqichen/cloud-foundry-cli-contexts/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/zongqichen/cloud-foundry-cli-contexts/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/zongqichen/cloud-foundry-cli-contexts/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/zongqichen/cloud-foundry-cli-contexts/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/zongqichen/cloud-foundry-cli-contexts/releases/tag/v0.1.0
