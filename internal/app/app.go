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
	"github.com/zongqichen/cfs/internal/workspace"
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

	_, hasExternalCFHome := externalCFHome()
	if envTrue(envvar.Disable) || os.Getenv(envvar.ActiveContext) != "" || hasExternalCFHome {
		return invokeOfficial(options, cfg.RealCFPath, args, os.Environ())
	}

	managed, err := resolveManagedContext(cfg)
	if err != nil {
		if errors.Is(err, workspace.ErrNotFound) {
			fprintf(options.Stderr, "cfs: no workspace could be resolved; refusing to use the global CF home\n")
			fprintf(options.Stderr, "Hint: run this command inside a Git worktree or set %s.\n", envvar.WorkspaceRoot)
		} else {
			fprintf(options.Stderr, "cfs: %v\n", err)
		}
		return exitUnavailable
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
	if err := workspaceLock.Release(); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		if exitCode == exitOK {
			return exitError
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
