package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/envvar"
	"github.com/zongqichen/cfs/internal/lock"
	"github.com/zongqichen/cfs/internal/runner"
	"github.com/zongqichen/cfs/internal/store"
	"github.com/zongqichen/cfs/internal/workspace"
)

const defaultLockTimeout = 3 * time.Second

type managedContext struct {
	Workspace workspace.Workspace
	Context   store.Context
	Store     store.Store
}

func resolveManagedContext(cfg config.Config) (managedContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return managedContext{}, fmt.Errorf("resolve current directory: %w", err)
	}
	ws, err := workspace.Resolve(cwd)
	if err != nil {
		return managedContext{}, err
	}
	return resolveManagedContextFromWorkspace(cfg, ws)
}

func reportWorkspaceError(options Options, err error) {
	if errors.Is(err, workspace.ErrNotFound) {
		fprintf(options.Stderr, "cfs: no workspace could be resolved; refusing to use the global CF home\n")
		fprintf(options.Stderr, "Hint: run this command inside a Git worktree or set %s.\n", envvar.WorkspaceRoot)
		return
	}
	fprintf(options.Stderr, "cfs: %v\n", err)
}

func resolveManagedContextFromWorkspace(cfg config.Config, ws workspace.Workspace) (managedContext, error) {
	stateRoot, err := config.StateRoot(cfg)
	if err != nil {
		return managedContext{}, err
	}
	stateStore := store.New(stateRoot)
	ctx, err := stateStore.ContextFor(ws)
	if err != nil {
		return managedContext{}, err
	}
	return managedContext{Workspace: ws, Context: ctx, Store: stateStore}, nil
}

func (managed managedContext) activate(timeout time.Duration) (*lock.Lock, error) {
	if err := managed.Store.Prepare(managed.Context); err != nil {
		return nil, err
	}
	workspaceLock, err := lock.Acquire(managed.Context.LockPath, timeout)
	if err != nil {
		return nil, err
	}
	if err := managed.Store.Ensure(managed.Context, managed.Workspace); err != nil {
		return nil, errors.Join(err, workspaceLock.Release())
	}
	return workspaceLock, nil
}

func (managed managedContext) environment(base []string) []string {
	values := map[string]string{
		envvar.CFHome:        managed.Context.CFHome,
		envvar.ActiveContext: managed.Context.ID,
	}
	if os.Getenv(envvar.CFPluginHome) == "" {
		values[envvar.CFPluginHome] = managed.Store.SharedPluginHome()
	}
	return runner.ReplaceEnv(base, values)
}

func externalCFHome() (string, bool) {
	home := os.Getenv(envvar.CFHome)
	return home, home != "" && os.Getenv(envvar.WorkspaceRoot) == ""
}

func activeManagedEnvironment(cfg config.Config) (bool, error) {
	id := os.Getenv(envvar.ActiveContext)
	if id == "" {
		return false, nil
	}
	home := os.Getenv(envvar.CFHome)
	if home == "" {
		return false, fmt.Errorf("%s is set without %s", envvar.ActiveContext, envvar.CFHome)
	}

	stateRoot, err := config.StateRoot(cfg)
	if err != nil {
		return false, err
	}
	stateStore := store.New(stateRoot)
	ctx, err := stateStore.Context(id)
	if err != nil {
		return false, err
	}
	actualHome, err := filepath.Abs(home)
	if err != nil {
		return false, fmt.Errorf("resolve active %s: %w", envvar.CFHome, err)
	}
	if filepath.Clean(actualHome) != filepath.Clean(ctx.CFHome) {
		return false, fmt.Errorf("%s does not match %s", envvar.ActiveContext, envvar.CFHome)
	}
	_, err = stateStore.ValidateContext(ctx)
	if err != nil {
		return false, fmt.Errorf("validate active context: %w", err)
	}
	return true, nil
}

func lockTimeout() (time.Duration, error) {
	value := os.Getenv(envvar.LockTimeout)
	if value == "" {
		return defaultLockTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout < 0 {
		return 0, fmt.Errorf("invalid %s %q", envvar.LockTimeout, value)
	}
	return timeout, nil
}
