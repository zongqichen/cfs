package app

import (
	"errors"
	"os"

	"github.com/zongqichen/cfs/internal/cfhome"
	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/contextname"
	"github.com/zongqichen/cfs/internal/envvar"
	"github.com/zongqichen/cfs/internal/lock"
)

func commandImport(options Options, args []string) (exitCode int) {
	flags := newFlagSet("import", options.Stderr)
	yes := flags.Bool("yes", false, "confirm import without prompting")
	force := flags.Bool("force", false, "replace an existing workspace target")
	name := flags.String("context", contextname.Default, "destination context name")
	if code, ok := parseFlagSet(flags, args); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintf(options.Stderr, "cfs: import does not accept positional arguments\n")
		return exitUsage
	}
	if os.Getenv(envvar.CFHome) != "" {
		fprintf(options.Stderr, "cfs: unset %s before importing into a managed workspace\n", envvar.CFHome)
		return exitUsage
	}
	if err := contextname.Validate(*name); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}

	cfg, err := config.Load()
	if err != nil {
		return reportConfigError(options, err)
	}
	if err := validateRealCF(cfg.RealCFPath); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUnavailable
	}
	managed, err := resolveManagedContextForName(cfg, *name)
	if err != nil {
		reportWorkspaceError(options, err)
		return exitUnavailable
	}
	if *name != contextname.Default {
		if _, err := managed.Store.ValidateForWorkspace(managed.Context, managed.Workspace); errors.Is(err, os.ErrNotExist) {
			fprintf(options.Stderr, "cfs: context %q does not exist\n", *name)
			fprintf(options.Stderr, "Hint: run 'cfs context create %s' first.\n", *name)
			return exitUnavailable
		} else if err != nil {
			fprintf(options.Stderr, "cfs: inspect context %q: %v\n", *name, err)
			return exitError
		}
	}

	globalHome, available, err := globalTargetAvailable()
	if err != nil {
		fprintf(options.Stderr, "cfs: inspect global CF target: %v\n", err)
		return exitError
	}
	if !available {
		fprintf(options.Stderr, "cfs: no active global CF target found\n")
		fprintf(options.Stderr, "Hint: run 'CFS_DISABLE=1 cf login' first.\n")
		return exitUnavailable
	}
	timeout, err := lockTimeout()
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	workspaceLock, err := managed.activateSelected(timeout)
	if errors.Is(err, errContextNotFound) {
		fprintf(options.Stderr, "cfs: context %q does not exist\n", *name)
		return exitUnavailable
	}
	if errors.Is(err, lock.ErrBusy) {
		fprintf(options.Stderr, "cfs: context %q already has an active CF command\n", *name)
		return exitTemporary
	}
	if err != nil {
		fprintf(options.Stderr, "cfs: prepare workspace import: %v\n", err)
		return exitError
	}
	defer func() {
		if err := workspaceLock.Release(); err != nil {
			fprintf(options.Stderr, "cfs: %v\n", err)
			if exitCode == exitOK {
				exitCode = exitError
			}
		}
	}()

	existing, err := targetAvailable(cfg.RealCFPath, managed.Context.CFHome)
	if err != nil {
		fprintf(options.Stderr, "cfs: inspect workspace CF target: %v\n", err)
		return exitError
	}
	if existing && !*force {
		fprintf(options.Stderr, "cfs: workspace already has an active CF target; use --force to replace it\n")
		return exitUsage
	}
	if !*yes {
		prompt := "Import the global CF context, including any credentials, into context " + *name + " in " + managed.Workspace.Root + "?"
		if existing {
			prompt = "Replace context " + *name + " with the global CF context?"
		}
		confirmed, code := confirm(options, "import", prompt)
		if !confirmed {
			return code
		}
	}
	if err := cfhome.Import(globalHome, managed.Context.CFHome); err != nil {
		fprintf(options.Stderr, "cfs: import failed: %v\n", err)
		return exitError
	}
	available, err = targetAvailable(cfg.RealCFPath, managed.Context.CFHome)
	if err != nil || !available {
		fprintf(options.Stderr, "cfs: imported CF target could not be verified\n")
		return exitError
	}

	if *name == contextname.Default {
		fprintf(options.Stdout, "Imported global CF context into %s.\n", managed.Workspace.Root)
	} else {
		fprintf(options.Stdout, "Imported global CF context into %q.\n", *name)
	}
	return exitOK
}
