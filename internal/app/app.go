package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/lock"
	"github.com/zongqichen/cfs/internal/runner"
	"github.com/zongqichen/cfs/internal/store"
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

	name := strings.TrimSuffix(strings.ToLower(filepath.Base(options.Args[0])), ".exe")
	if name == "cf" {
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

	explicitWorkspace := os.Getenv("CFS_WORKSPACE_ROOT") != ""
	if envTrue("CFS_DISABLE") || os.Getenv("CFS_ACTIVE_CONTEXT") != "" || (os.Getenv("CF_HOME") != "" && !explicitWorkspace) {
		return invokeOfficial(options, cfg.RealCFPath, args, os.Environ())
	}

	cwd, err := os.Getwd()
	if err != nil {
		fprintf(options.Stderr, "cfs: resolve current directory: %v\n", err)
		return exitError
	}
	ws, err := workspace.Resolve(cwd)
	if err != nil {
		if errors.Is(err, workspace.ErrNotFound) {
			fprintf(options.Stderr, "cfs: no workspace could be resolved; refusing to use the global CF home\n")
			fprintf(options.Stderr, "Hint: run this command inside a Git worktree or set CFS_WORKSPACE_ROOT.\n")
		} else {
			fprintf(options.Stderr, "cfs: %v\n", err)
		}
		return exitUnavailable
	}

	stateRoot, err := config.StateRoot(cfg)
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}
	stateStore := store.New(stateRoot)
	ctx, err := stateStore.ContextFor(ws)
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}
	if err := stateStore.Prepare(ctx); err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}

	timeout, err := lockTimeout()
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitUsage
	}
	workspaceLock, err := lock.Acquire(ctx.LockPath, timeout)
	if errors.Is(err, lock.ErrBusy) {
		fprintf(options.Stderr, "cfs: this workspace already has an active CF command\n")
		fprintf(options.Stderr, "Hint: wait for the command to finish or use a separate Git worktree.\n")
		return exitTemporary
	}
	if err != nil {
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}

	if err := stateStore.Ensure(ctx, ws); err != nil {
		_ = workspaceLock.Release()
		fprintf(options.Stderr, "cfs: %v\n", err)
		return exitError
	}

	values := map[string]string{
		"CF_HOME":            ctx.CFHome,
		"CFS_ACTIVE_CONTEXT": ctx.ID,
	}
	if os.Getenv("CF_PLUGIN_HOME") == "" {
		values["CF_PLUGIN_HOME"] = stateStore.SharedPluginHome()
	}
	env := runner.ReplaceEnv(os.Environ(), values)
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
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("official CF CLI is unavailable at %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("official CF CLI is not a regular file: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("official CF CLI is not executable: %s", path)
	}
	target, targetErr := filepath.EvalSymlinks(path)
	self, selfErr := os.Executable()
	if targetErr == nil && selfErr == nil {
		self, selfErr = filepath.EvalSymlinks(self)
	}
	if targetErr == nil && selfErr == nil && pathsEqual(target, self) {
		return fmt.Errorf("official CF CLI path resolves to cfs itself: %s", path)
	}
	return nil
}

func lockTimeout() (time.Duration, error) {
	value := os.Getenv("CFS_LOCK_TIMEOUT")
	if value == "" {
		return 3 * time.Second, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout < 0 {
		return 0, fmt.Errorf("invalid CFS_LOCK_TIMEOUT %q", value)
	}
	return timeout, nil
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
