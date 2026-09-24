// Command cfs isolates Cloud Foundry CLI state per project or Git worktree.
//
// After setup, its transparent cf shim selects a private CF_HOME for each
// workspace and delegates every Cloud Foundry operation to the official cf CLI.
// Named contexts provide additional isolated targets within one workspace.
package main
