package app

import (
	"context"
	"strings"

	"github.com/zongqichen/cfs/internal/envvar"
	"github.com/zongqichen/cfs/internal/updatecheck"
)

func commandUpdate(options Options, args []string) int {
	flags := newFlagSet("update", options.Stderr)
	jsonOutput := flags.Bool("json", false, "print JSON output")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: update does not accept positional arguments\n")
		return exitUsage
	}
	if options.Updates == nil {
		fprintf(options.Stderr, "cfs: update checking is unavailable in this build\n")
		return exitUnavailable
	}

	result, err := options.Updates.Check(context.Background(), options.Version)
	if err != nil {
		fprintf(options.Stderr, "cfs: check for updates: %v\n", err)
		return exitUnavailable
	}
	if *jsonOutput {
		return writeJSON(options, result)
	}
	printUpdateResult(options, result)
	return exitOK
}

func printUpdateResult(options Options, result updatecheck.Result) {
	switch result.Status {
	case updatecheck.StatusUpdateAvailable:
		fprintf(options.Stdout, "Update available: %s -> %s\n", result.CurrentVersion, result.LatestVersion)
		fprintf(options.Stdout, "Release: %s\n", result.ReleaseURL)
		fprintf(options.Stdout, "Update with the same installation method, or run:\n")
		for _, command := range result.Commands {
			fprintf(options.Stdout, "  %s\n", command)
		}
	case updatecheck.StatusUpToDate:
		fprintf(options.Stdout, "cfs %s is up to date.\n", result.CurrentVersion)
	case updatecheck.StatusAhead:
		fprintf(options.Stdout, "cfs %s is newer than the latest published release (%s).\n", result.CurrentVersion, result.LatestVersion)
	default:
		fprintf(options.Stdout, "Current build %s cannot be compared with published releases.\n", result.CurrentVersion)
		fprintf(options.Stdout, "Latest release: %s\n", result.LatestVersion)
		fprintf(options.Stdout, "Release: %s\n", result.ReleaseURL)
	}
}

func maybeNotifyUpdate(options Options, command commandSpec, args []string) {
	if !shouldNotifyUpdate(options, command, args) {
		return
	}
	result, notify, err := options.Updates.Notification(context.Background(), options.Version)
	if err != nil || !notify {
		return
	}
	fprintf(options.Stderr, "cfs: update available: %s -> %s; run 'cfs update'\n", result.CurrentVersion, result.LatestVersion)
}

func shouldNotifyUpdate(options Options, command commandSpec, args []string) bool {
	if options.Updates == nil || isHelpRequest(args) || hasJSONFlag(args) {
		return false
	}
	switch command.name {
	case "help", "uninstall", "update":
		return false
	}
	if envTrue(envvar.NoUpdateCheck) || envTrue(envvar.CI) {
		return false
	}
	return options.IsInteractive != nil && options.IsInteractive(options.Stderr)
}

func hasJSONFlag(args []string) bool {
	for _, argument := range args {
		if argument == "--json" || argument == "-json" || strings.HasPrefix(argument, "--json=") || strings.HasPrefix(argument, "-json=") {
			return true
		}
	}
	return false
}
