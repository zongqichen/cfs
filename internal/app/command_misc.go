package app

import "github.com/zongqichen/cfs/internal/install"

func commandUninstall(options Options, args []string) int {
	flags := newFlagSet("uninstall", options.Stderr)
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: uninstall does not accept positional arguments\n")
		return exitUsage
	}
	path, err := install.Uninstall()
	if err != nil {
		fprintf(options.Stderr, "cfs: uninstall failed: %v\n", err)
		return exitError
	}
	fprintf(options.Stdout, "Removed shim: %s\n", path)
	fprintf(options.Stdout, "Workspace state and the official CF CLI were preserved.\n")
	return exitOK
}

func commandVersion(options Options, args []string) int {
	flags := newFlagSet("version", options.Stderr)
	jsonOutput := flags.Bool("json", false, "print JSON output")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: version does not accept positional arguments\n")
		return exitUsage
	}
	value := map[string]string{
		"version":    options.Version,
		"commit":     options.Commit,
		"build_date": options.BuildDate,
	}
	if *jsonOutput {
		return writeJSON(options, value)
	}
	fprintf(options.Stdout, "cfs %s (commit %s, built %s)\n", options.Version, options.Commit, options.BuildDate)
	return exitOK
}
