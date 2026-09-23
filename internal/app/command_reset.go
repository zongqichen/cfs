package app

import (
	"errors"
	"os"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/envvar"
	"github.com/zongqichen/cfs/internal/lock"
)

func commandReset(options Options, args []string) int {
	flags := newFlagSet("reset", options.Stderr)
	yes := flags.Bool("yes", false, "confirm reset without prompting")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: reset does not accept positional arguments\n")
		return exitUsage
	}
	if os.Getenv(envvar.CFHome) != "" {
		fprintf(options.Stderr, "cfs: refusing to reset an externally managed %s\n", envvar.CFHome)
		return exitUsage
	}

	cfg, err := config.Load()
	if err != nil {
		return reportConfigError(options, err)
	}
	managed, err := resolveManagedContext(cfg)
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUnavailable
	}
	if _, err := os.Stat(managed.Context.Dir); errors.Is(err, os.ErrNotExist) {
		fprintf(options.Stdout, "No managed CF state exists for %s.\n", managed.Workspace.Root)
		return exitOK
	} else if err != nil {
		fprintf(options.Stderr, "cfs: inspect context: %v\n", err)
		return exitError
	}

	if !*yes {
		confirmed, code := confirmReset(options, managed.Workspace.Root)
		if !confirmed {
			return code
		}
	}
	timeout, err := lockTimeout()
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	workspaceLock, err := lock.Acquire(managed.Context.LockPath, timeout)
	if err != nil {
		fprintf(options.Stderr, "cfs: cannot reset active workspace: %v\n", err)
		return exitTemporary
	}
	destination, moveErr := managed.Store.MoveToTrash(managed.Context)
	releaseErr := workspaceLock.Release()
	if moveErr != nil {
		fprintf(options.Stderr, "cfs: %v\n", moveErr)
		return exitError
	}
	if releaseErr != nil {
		fprintf(options.Stderr, "cfs: %v\n", releaseErr)
		return exitError
	}
	fprintf(options.Stdout, "Moved workspace state to %s\n", destination)
	fprintf(options.Stdout, "%s\n", trashCredentialWarning)
	return exitOK
}

func confirmReset(options Options, workspaceRoot string) (bool, int) {
	return confirm(options, "reset", "Reset CF state for "+workspaceRoot+"?")
}
