package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/envvar"
	"github.com/zongqichen/cfs/internal/executable"
	"github.com/zongqichen/cfs/internal/lock"
	"github.com/zongqichen/cfs/internal/runner"
)

const (
	exitOK          = 0
	exitError       = 1
	exitUsage       = 64
	exitUnavailable = 69
	exitTemporary   = 75
)

type Options struct {
	Args      []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Version   string
	Commit    string
	BuildDate string
}

func Run(options Options) int {
	options = withDefaultStreams(options)
	if len(options.Args) == 0 {
		fprintf(options.Stderr, "cfs: missing process name\n")
		return exitError
	}

	name := filepath.Base(options.Args[0])
	if strings.EqualFold(name, executable.Name("cf")) {
		return runShim(options, options.Args[1:])
	}
	return runControl(options, options.Args[1:])
}

func runShim(options Options, args []string) int {
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotConfigured) {
			fprintf(options.Stderr, "cfs: not configured; run 'cfs setup' first\n")
		} else {
			fprintf(options.Stderr, "cfs: %v\n", err)
		}
		return exitUnavailable
	}
	if err := validateRealCF(cfg.RealCFPath); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUnavailable
	}

	if envTrue(envvar.Disable) {
		return invokeOfficial(options, cfg.RealCFPath, args, os.Environ())
	}
	active, err := activeManagedEnvironment(cfg)
	if err != nil {
		fprintf(options.Stderr, "cfs: refusing invalid active context: %v\n", err)
		return exitUnavailable
	}
	_, hasExternalCFHome := externalCFHome()
	if active || hasExternalCFHome {
		return invokeOfficial(options, cfg.RealCFPath, args, os.Environ())
	}

	managed, err := resolveManagedContext(cfg)
	if err != nil {
		reportWorkspaceError(options, err)
		return exitUnavailable
	}
	workspaceWasEmpty, err := workspaceConfigMissing(managed.Context.CFHome)
	if err != nil {
		fprintf(options.Stderr, "cfs: inspect workspace CF state: %v\n", err)
		return exitError
	}

	timeout, err := lockTimeout()
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	workspaceLock, err := managed.activate(timeout)
	if errors.Is(err, lock.ErrBusy) {
		fprintf(options.Stderr, "cfs: this workspace already has an active CF command\n")
		fprintf(options.Stderr, "Hint: wait for the command to finish or use a separate Git worktree.\n")
		return exitTemporary
	}
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}

	env := managed.environment(os.Environ())
	exitCode := invokeOfficial(options, cfg.RealCFPath, args, env)
	commandFailed := exitCode != exitOK
	if err := workspaceLock.Release(); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		if exitCode == exitOK {
			return exitError
		}
	}
	if commandFailed && workspaceWasEmpty {
		if _, available, _ := globalTargetAvailable(); available {
			fprintf(options.Stderr, "cfs: a global CF context is available; run 'cfs import' to use it here.\n")
		}
	}
	return exitCode
}

func invokeOfficial(options Options, path string, args []string, env []string) int {
	result, err := runner.Run(path, args, env, runner.IO{
		Stdin: options.Stdin, Stdout: options.Stdout, Stderr: options.Stderr,
	})
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUnavailable
	}
	return result.ExitCode
}

func validateRealCF(path string) error {
	real, err := executable.Resolve(path)
	if err != nil {
		return fmt.Errorf("official CF CLI is unavailable at %s: %w", path, err)
	}
	self, selfErr := executable.Current()
	if selfErr == nil && executable.Same(real, self) {
		return fmt.Errorf("official CF CLI path resolves to cfs itself: %s", path)
	}
	return nil
}

func envTrue(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func withDefaultStreams(options Options) Options {
	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	return options
}

func fprintf(writer io.Writer, format string, values ...any) {
	_, _ = fmt.Fprintf(writer, format, values...)
}
