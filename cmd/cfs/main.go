package main

import (
	"os"
	"runtime/debug"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/app"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/updatecheck"
)

var version = "dev"
var commit = "unknown"
var buildDate = "unknown"

func main() {
	resolvedVersion, resolvedCommit, resolvedBuildDate := effectiveBuildMetadata(version, commit, buildDate)
	os.Exit(app.Run(app.Options{
		Args:      os.Args,
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Version:   resolvedVersion,
		Commit:    resolvedCommit,
		BuildDate: resolvedBuildDate,
		Updates:   updatecheck.New(updatecheck.Options{}),
	}))
}

func effectiveBuildMetadata(currentVersion, currentCommit, currentBuildDate string) (string, string, string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return currentVersion, currentCommit, currentBuildDate
	}
	return resolveBuildMetadata(currentVersion, currentCommit, currentBuildDate, info)
}

func resolveBuildMetadata(currentVersion, currentCommit, currentBuildDate string, info *debug.BuildInfo) (string, string, string) {
	if currentVersion == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		currentVersion = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if currentCommit == "unknown" && setting.Value != "" {
				currentCommit = setting.Value
			}
		case "vcs.time":
			if currentBuildDate == "unknown" && setting.Value != "" {
				currentBuildDate = setting.Value
			}
		}
	}
	return currentVersion, currentCommit, currentBuildDate
}
