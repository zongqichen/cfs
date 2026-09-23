package app

import (
	"path/filepath"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/install"
)

func commandSetup(options Options, args []string) int {
	flags := newFlagSet("setup", options.Stderr)
	realCF := flags.String("real-cf", "", "path to the official CF CLI")
	shimDir := flags.String("shim-dir", "", "directory in which to install the cf shim")
	stateDir := flags.String("state-dir", "", "override the cfs state directory")
	jsonOutput := flags.Bool("json", false, "print JSON output")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: setup does not accept positional arguments\n")
		return exitUsage
	}

	result, err := install.Setup(install.SetupOptions{
		RealCFPath: *realCF, ShimDir: *shimDir, StateDir: *stateDir,
	})
	if err != nil {
		fprintf(options.Stderr, "cfs: setup failed: %v\n", err)
		return exitError
	}
	if *jsonOutput {
		return writeJSON(options, result)
	}

	fprintf(options.Stdout, "Found official CF CLI: %s\n", result.RealCFPath)
	fprintf(options.Stdout, "Installed shim: %s\n", result.ShimPath)
	fprintf(options.Stdout, "Saved configuration: %s\n", result.ConfigPath)
	if result.PathReady {
		fprintf(options.Stdout, "PATH is ready. Run 'cfs doctor' to verify the installation.\n")
	} else {
		fprintf(options.Stdout, "Add this directory to the beginning of PATH: %s\n", filepath.Dir(result.ShimPath))
	}
	return exitOK
}
