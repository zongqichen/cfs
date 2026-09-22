package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveBuildMetadataUsesGoModuleInformation(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.1.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-09-22T12:00:00Z"},
		},
	}

	gotVersion, gotCommit, gotBuildDate := resolveBuildMetadata("dev", "unknown", "unknown", info)
	if gotVersion != "v0.1.0" || gotCommit != "0123456789abcdef" || gotBuildDate != "2026-09-22T12:00:00Z" {
		t.Fatalf("metadata = %q, %q, %q", gotVersion, gotCommit, gotBuildDate)
	}
}

func TestResolveBuildMetadataPreservesLinkerValues(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.1.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "module-commit"},
			{Key: "vcs.time", Value: "2026-09-22T12:00:00Z"},
		},
	}

	gotVersion, gotCommit, gotBuildDate := resolveBuildMetadata("v1.2.3", "release-commit", "release-date", info)
	if gotVersion != "v1.2.3" || gotCommit != "release-commit" || gotBuildDate != "release-date" {
		t.Fatalf("metadata = %q, %q, %q", gotVersion, gotCommit, gotBuildDate)
	}
}
