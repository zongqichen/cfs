package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/contextname"
	"github.com/zongqichen/cfs/internal/envvar"
	"github.com/zongqichen/cfs/internal/lock"
)

func parseNamedInvocation(args []string) (string, []string, bool, error) {
	if len(args) == 0 {
		return "", nil, false, nil
	}
	var name string
	switch {
	case args[0] == "-c" || args[0] == "--context":
		if len(args) < 2 {
			return "", nil, true, errors.New("-c/--context requires a name")
		}
		name = args[1]
		args = args[2:]
	case strings.HasPrefix(args[0], "--context="):
		name = strings.TrimPrefix(args[0], "--context=")
		args = args[1:]
	default:
		return "", nil, false, nil
	}
	if err := contextname.Validate(name); err != nil {
		return "", nil, true, err
	}
	if len(args) == 0 {
		return "", nil, true, fmt.Errorf("context %q requires a CF command", name)
	}
	return name, args, true, nil
}

func runNamed(options Options, name string, args []string) int {
	if os.Getenv(envvar.CFHome) != "" {
		fprintf(options.Stderr, "cfs: unset %s before selecting a named context\n", envvar.CFHome)
		return exitUsage
	}
	if envTrue(envvar.Disable) {
		fprintf(options.Stderr, "cfs: unset %s before selecting a named context\n", envvar.Disable)
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
	managed, err := resolveManagedContextForName(cfg, name)
	if err != nil {
		reportWorkspaceError(options, err)
		return exitUnavailable
	}

	timeout, err := lockTimeout()
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	contextLock, err := managed.activateSelected(timeout)
	if errors.Is(err, errContextNotFound) {
		fprintf(options.Stderr, "cfs: context %q does not exist\n", name)
		fprintf(options.Stderr, "Hint: run 'cfs context create %s' first.\n", name)
		return exitUnavailable
	}
	if errors.Is(err, lock.ErrBusy) {
		fprintf(options.Stderr, "cfs: context %q already has an active CF command\n", name)
		return exitTemporary
	}
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}
	workspaceWasEmpty, err := workspaceConfigMissing(managed.Context.CFHome)
	if err != nil {
		releaseErr := contextLock.Release()
		fprintf(options.Stderr, "cfs: inspect context %q CF state: %v\n", name, errors.Join(err, releaseErr))
		return exitError
	}

	env := managed.environment(os.Environ())
	exitCode := invokeOfficial(options, cfg.RealCFPath, args, env)
	if releaseErr := contextLock.Release(); releaseErr != nil {
		fprintf(options.Stderr, "cfs: %v\n", releaseErr)
		if exitCode == exitOK {
			exitCode = exitError
		}
	}
	if exitCode != exitOK && workspaceWasEmpty {
		if _, available, _ := globalTargetAvailable(); available {
			fprintf(options.Stderr, "cfs: a global CF context is available; run 'cfs import --context %s' to use it here.\n", name)
		}
	}
	return exitCode
}
